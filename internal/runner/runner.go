// Package runner does the per-server work in phases.
//
// check (read-only, nothing on the server changes):
//
//	connect   SSH login (password / key, host key verified)
//	sudo      sudo works with the configured password
//	health    every target's health checks pass
//	files     every destination is usable; is it already the new certificate?
//	state     live state outside files is current (Kubernetes secrets)
//	tls       which certificate every VERIFY port serves right now
//
// deploy = check, then:
//
//	upload    new files to a private temp dir, sha256 verified
//	replace   backup to <file>.bak-<stamp>, overwrite in place (owner and
//	          mode kept), sha256 verified
//	reload    each changed target's reload, then the server's POST
//	health    health checks again
//	verify    every VERIFY port must serve the NEW certificate
//
// A failure after replace triggers a rollback: old files restored, new files
// removed, each reloaded target's rollback (or reload again) so services use
// the old certificate again.
package runner

import (
	"crypto/rand"
	"crypto/x509"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/ilramdhan/sslsync/internal/certs"
	"github.com/ilramdhan/sslsync/internal/inventory"
	"github.com/ilramdhan/sslsync/internal/remote"
	"github.com/ilramdhan/sslsync/internal/report"
	"github.com/ilramdhan/sslsync/internal/tlsprobe"
)

type Status string

const (
	StatusReady          Status = "ready"           // check: healthy, needs the new certificate
	StatusUpToDate       Status = "up-to-date"      // already has the new certificate everywhere
	StatusUpdated        Status = "updated"         // deploy: done and verified
	StatusPlanned        Status = "planned"         // plan
	StatusCheckFailed    Status = "check-failed"    // nothing was changed
	StatusRolledBack     Status = "rolled-back"     // failed after replace; old certificate restored
	StatusRollbackFailed Status = "ROLLBACK-FAILED" // needs manual action
)

// OK reports whether the status is a success.
func (s Status) OK() bool {
	switch s {
	case StatusReady, StatusUpToDate, StatusUpdated, StatusPlanned:
		return true
	}
	return false
}

type Mode int

const (
	ModeCheck Mode = iota
	ModeDeploy
	ModePlan
)

func (m Mode) String() string {
	return [...]string{"check", "deploy", "plan"}[m]
}

// Runner holds what is shared by all servers of one run.
type Runner struct {
	Bundles    map[string]*certs.Bundle // see BundleKey
	Mode       Mode
	Stamp      string
	Retries    int
	KnownHosts string
}

// BundleKey identifies the certificate pair a server deploys.
func BundleKey(s *inventory.Server) string { return s.CertFile + "\x00" + s.KeyFile }

// server is the state of one server while it is processed.
type server struct {
	*Runner
	srv    *inventory.Server
	bundle *certs.Bundle
	log    *report.Logger
	res    report.ServerResult
	client *remote.Client

	staged   []staged
	replaced []string          // dests backed up to dest.bak-<stamp>
	created  []string          // dests that did not exist before
	changed  map[string]bool   // target IDs whose files changed or whose live state is outdated
	reloaded []string          // target IDs whose reload was started
	modeFix  map[string]string // dest -> mode a key file must be tightened to
	modeOld  map[string]string // dest -> mode it had, for rollback

	fixedModes []string // dests whose mode was tightened (undone on rollback)
}

type staged struct {
	inventory.File
	sha, tmp string
}

// Run processes one server and returns its result.
func (r *Runner) Run(srv *inventory.Server, log *report.Logger) report.ServerResult {
	s := &server{Runner: r, srv: srv, log: log, bundle: r.Bundles[BundleKey(srv)], changed: map[string]bool{},
		modeFix: map[string]string{}, modeOld: map[string]string{}}
	s.res = report.ServerResult{Server: srv.Name, Group: srv.Group, Host: srv.Host, Started: time.Now().Format(time.RFC3339)}
	start := time.Now()

	status, err := s.run()

	s.res.Status = string(status)
	s.res.OK = status.OK()
	if err != nil {
		s.res.Error = err.Error()
		log.Logf("✗ %s: %v", status, err)
	} else {
		log.Logf("● %s", status)
	}
	s.res.Duration = time.Since(start).Round(time.Millisecond).String()
	if s.client != nil {
		if err := s.client.Close(); err != nil {
			log.Logf("! closing SSH connection: %v", err)
		}
	}
	return s.res
}

func (s *server) run() (Status, error) {
	if s.Mode == ModePlan {
		s.plan()
		return StatusPlanned, nil
	}

	// ---- check (read-only) ----
	if err := s.phase("connect", s.connect); err != nil {
		return StatusCheckFailed, err
	}
	if s.srv.Sudo {
		if err := s.phase("sudo", s.checkSudo); err != nil {
			return StatusCheckFailed, err
		}
	}
	if err := s.phase("health", s.health); err != nil {
		return StatusCheckFailed, err
	}
	if err := s.phase("files", s.inspectFiles); err != nil {
		return StatusCheckFailed, err
	}
	if err := s.phase("state", s.inspectState); err != nil {
		return StatusCheckFailed, err
	}
	allNew := true
	if len(s.srv.Verify) > 0 {
		// informational only; never fails the check
		_ = s.phase("tls", func() (string, error) { allNew = s.reportTLS(); return "", nil })
	}
	upToDate := len(s.changed) == 0 && allNew && len(s.modeFix) == 0

	if s.Mode == ModeCheck {
		if upToDate {
			return StatusUpToDate, nil
		}
		return StatusReady, nil
	}

	// ---- deploy ----
	if upToDate {
		s.log.Logf("= everything already has the new certificate, nothing to do")
		return StatusUpToDate, nil
	}
	if len(s.changed) == 0 && !allNew {
		// files and secrets are new but a port still serves the old cert:
		// an earlier reload did not happen or did not work — reload all.
		for _, t := range s.srv.Targets {
			s.changed[t.ID] = true
		}
		s.log.Logf("~ files are current but a port serves the old certificate — reloading every target")
	}
	defer s.cleanupTmp()
	if err := s.phase("upload", s.upload); err != nil {
		return StatusCheckFailed, err
	}
	if err := s.phase("replace", s.replace); err != nil {
		return s.rollback(err)
	}
	if err := s.phase("reload", s.reload); err != nil {
		return s.rollback(err)
	}
	if err := s.phase("health", s.health); err != nil {
		return s.rollback(err)
	}
	if len(s.srv.Verify) > 0 {
		if err := s.phase("verify", s.verify); err != nil {
			return s.rollback(err)
		}
	}
	s.dropBackups()
	return StatusUpdated, nil
}

// phase runs fn, logs and records its outcome.
func (s *server) phase(name string, fn func() (string, error)) error {
	start := time.Now()
	detail, err := fn()
	p := report.Phase{Name: name, OK: err == nil, Detail: detail, Duration: time.Since(start).Round(time.Millisecond).String()}
	if err != nil {
		p.Detail = err.Error()
		s.log.Logf("✗ %-8s %v", name, err)
	} else if detail != "" {
		s.log.Logf("✓ %-8s %s", name, detail)
	}
	s.res.Phases = append(s.res.Phases, p)
	return err
}

// exec runs a command as configured for the server (sudo, timeout) with
// the target's settings and the run stamp exported.
func (s *server) exec(cmd string, t *inventory.Target, timeout time.Duration) (string, error) {
	env := map[string]string{"SSLSYNC_STAMP": s.Stamp, "SSLSYNC_SERVER": s.srv.Name}
	if t != nil {
		for k, v := range t.Env {
			env[k] = v
		}
	}
	return s.client.Run(cmd, remote.RunOpts{Sudo: s.srv.Sudo, Timeout: timeout, Env: env})
}

func (s *server) connect() (string, error) {
	c, err := remote.Dial(s.srv, s.KnownHosts, s.Retries, s.log.Logf)
	if err != nil {
		return "", err
	}
	s.client = c
	out, _ := c.Run(`. /etc/os-release 2>/dev/null; echo "$(hostname) | ${PRETTY_NAME:-unknown OS} | up $(uptime -p 2>/dev/null | sed 's/^up //')"`, remote.RunOpts{})
	return strings.TrimSpace(out), nil
}

func (s *server) checkSudo() (string, error) {
	_, err := s.client.Run("true", remote.RunOpts{Sudo: true})
	return "ok", err
}

func (s *server) health() (string, error) {
	n := 0
	for i := range s.srv.Targets {
		t := &s.srv.Targets[i]
		for _, hc := range t.Health {
			if _, err := s.exec(hc, t, 2*time.Minute); err != nil {
				return "", fmt.Errorf("%s: %s: %w", t.ID, oneLine(hc), err)
			}
			n++
		}
	}
	if s.srv.Health != "" {
		if _, err := s.exec(s.srv.Health, nil, 2*time.Minute); err != nil {
			return "", fmt.Errorf("HEALTH: %w", err)
		}
		n++
	}
	if n == 0 {
		return "no health checks configured", nil
	}
	return fmt.Sprintf("%d check(s) passed", n), nil
}

// inspectFiles validates every destination and records which targets need
// new files. A missing file is a config error unless it may be created.
func (s *server) inspectFiles() (string, error) {
	var current int
	for _, f := range s.srv.Files() {
		script := fmt.Sprintf(`dst=%s
if [ -e "$dst" ]; then
  [ -f "$dst" ] || { echo "exists but is not a regular file" >&2; exit 1; }
  [ -w "$dst" ] || { echo "not writable — set <SERVER>_SUDO=true" >&2; exit 1; }
  [ -w "$(dirname "$dst")" ] || { echo "folder not writable, cannot create backup" >&2; exit 1; }
  echo "EXISTS $(sha256sum "$dst" | cut -d' ' -f1) $(stat -c '%%U:%%G %%a' "$dst")"
else
  %s
  d=$(dirname "$dst"); while [ ! -d "$d" ]; do d=$(dirname "$d"); done
  [ -w "$d" ] || { echo "cannot create files in $d — set <SERVER>_SUDO=true" >&2; exit 1; }
  echo MISSING
fi`, remote.Shq(f.Dest), missingPolicy(s.srv.Key, f))
		out, err := s.exec(script, nil, 0)
		if err != nil {
			return "", fmt.Errorf("%s: %w", f.Dest, err)
		}
		fields := strings.Fields(out)
		if len(fields) == 4 {
			if fix := f.SecureMode(parseMode(fields[3])); fix != "" {
				s.modeFix[f.Dest] = fix
				s.modeOld[f.Dest] = fields[3]
				s.log.Logf("  ! %-44s private key mode %s is too open, will be set to %s", f.Dest, fields[3], fix)
			}
		}
		switch {
		case len(fields) == 1 && fields[0] == "MISSING":
			s.changed[f.Target] = true
			s.log.Logf("  + %-44s missing, will be created (%s)", f.Dest, f.Kind)
		case len(fields) == 4 && fields[1] == s.bundle.SHA(f.Kind):
			current++
			s.log.Logf("  = %-44s already new (%s %s)", f.Dest, fields[2], fields[3])
		case len(fields) == 4:
			s.changed[f.Target] = true
			s.log.Logf("  ~ %-44s will be replaced (%s %s, %s)", f.Dest, fields[2], fields[3], f.Kind)
		default:
			return "", fmt.Errorf("%s: unexpected output %q", f.Dest, out)
		}
	}
	summary := fmt.Sprintf("%d current, %d to update", current, len(s.srv.Files())-current)
	if n := len(s.modeFix); n > 0 {
		summary += fmt.Sprintf(", %d key permission(s) to tighten", n)
	}
	return summary, nil
}

func parseMode(s string) os.FileMode {
	m, _ := strconv.ParseUint(s, 8, 32)
	return os.FileMode(m)
}

func missingPolicy(server string, f inventory.File) string {
	if f.Create {
		return ":"
	}
	return fmt.Sprintf(`echo "does not exist — check the path, or set %s_%s_CREATE=true for a new file" >&2; exit 1`, server, f.Target)
}

// inspectState asks targets with live state (Kubernetes secrets) whether it
// matches the files, so a deploy interrupted after replace gets completed.
func (s *server) inspectState() (string, error) {
	var outdated []string
	for i := range s.srv.Targets {
		t := &s.srv.Targets[i]
		if t.State == "" {
			continue
		}
		out, err := s.exec(t.State, t, 2*time.Minute)
		if err != nil {
			return "", fmt.Errorf("%s: %w", t.ID, err)
		}
		for _, line := range strings.Split(out, "\n") {
			if what, ok := strings.CutPrefix(strings.TrimSpace(line), "OUTDATED "); ok {
				s.changed[t.ID] = true
				outdated = append(outdated, what)
				s.log.Logf("  ~ %-44s outdated", what)
			}
		}
	}
	if len(outdated) == 0 {
		return "", nil
	}
	return fmt.Sprintf("%d outdated", len(outdated)), nil
}

// reportTLS logs what every VERIFY port serves now. Read-only.
func (s *server) reportTLS() bool {
	allNew := true
	for _, addr := range s.srv.Verify {
		c, err := tlsprobe.Fetch(addr)
		switch {
		case err != nil:
			allNew = false
			s.log.Logf("  ? %-44s unreachable: %v", addr, err)
		case c.Equal(s.bundle.Leaf):
			s.log.Logf("  = %-44s NEW (expires %s)", addr, c.NotAfter.Format(time.DateOnly))
		default:
			allNew = false
			s.log.Logf("  ~ %-44s OLD (expires %s)", addr, c.NotAfter.Format(time.DateOnly))
		}
	}
	return allNew
}

func (s *server) upload() (string, error) {
	dir := fmt.Sprintf("/tmp/.sslsync-%s-%s", s.Stamp, rand.Text()[:8])
	if _, err := s.client.Run("umask 077 && mkdir "+remote.Shq(dir), remote.RunOpts{}); err != nil {
		return "", fmt.Errorf("create temp dir: %w", err)
	}
	for i, f := range s.srv.Files() {
		st := staged{File: f, sha: s.bundle.SHA(f.Kind), tmp: fmt.Sprintf("%s/%d", dir, i)}
		s.staged = append(s.staged, st)
		cmd := fmt.Sprintf(`cat > %[1]s && [ "$(sha256sum %[1]s | cut -d' ' -f1)" = %[2]s ] || { echo "checksum mismatch after upload" >&2; exit 1; }`, remote.Shq(st.tmp), st.sha)
		if _, err := s.client.Run(cmd, remote.RunOpts{Stdin: s.bundle.Data(f.Kind)}); err != nil {
			return "", fmt.Errorf("upload for %s: %w", f.Dest, err)
		}
	}
	return fmt.Sprintf("%d file(s) staged, sha256 verified", len(s.staged)), nil
}

func (s *server) replace() (string, error) {
	for _, f := range s.staged {
		owner := ":"
		if f.Owner != "" {
			owner = `chown ` + remote.Shq(f.Owner) + ` "$dst"`
		}
		tighten := ":"
		if m := s.modeFix[f.Dest]; m != "" {
			tighten = "chmod " + m + ` "$dst"; echo MODE_FIXED`
		}
		script := fmt.Sprintf(`set -e
tmp=%[1]s; dst=%[2]s; bak="$dst.bak-%[3]s"
if [ ! -e "$dst" ]; then
  mkdir -p "$(dirname "$dst")"
  (umask 077; cat "$tmp" > "$dst"); chmod %[5]s "$dst"; %[6]s
  [ "$(sha256sum "$dst" | cut -d' ' -f1)" = %[4]s ] || { echo "verify after write failed" >&2; exit 1; }
  echo CREATED; exit 0
fi
if cmp -s "$tmp" "$dst"; then %[7]s; echo UNCHANGED; exit 0; fi
cp -p "$dst" "$bak"
echo BACKED_UP
cat "$tmp" > "$dst"
%[7]s
[ "$(sha256sum "$dst" | cut -d' ' -f1)" = %[4]s ] || { echo "verify after write failed" >&2; exit 1; }
echo REPLACED`, remote.Shq(f.tmp), remote.Shq(f.Dest), s.Stamp, f.sha, f.Mode(), owner, tighten)

		out, err := s.exec(script, nil, 0)
		if strings.Contains(out, "MODE_FIXED") {
			s.fixedModes = append(s.fixedModes, f.Dest)
			s.log.Logf("  ✓ %s permissions %s → %s", f.Dest, s.modeOld[f.Dest], s.modeFix[f.Dest])
		}
		switch {
		case strings.Contains(out, "CREATED"):
			s.created = append(s.created, f.Dest)
			s.log.Logf("  + %s created (mode %s)", f.Dest, f.Mode())
		case strings.Contains(out, "BACKED_UP"):
			// a backup exists even if the write then failed — restore from it
			s.replaced = append(s.replaced, f.Dest)
			if err == nil {
				s.log.Logf("  ✓ %s replaced (backup %s.bak-%s)", f.Dest, filepath.Base(f.Dest), s.Stamp)
			}
		case strings.Contains(out, "UNCHANGED"):
		case err != nil && f.Create:
			s.created = append(s.created, f.Dest) // may have failed half-way through creating
		}
		if err != nil {
			return "", fmt.Errorf("%s: %w", f.Dest, err)
		}
	}
	s.res.Changes = slices.Concat(s.replaced, s.created)
	summary := fmt.Sprintf("%d replaced, %d created", len(s.replaced), len(s.created))
	if n := len(s.fixedModes); n > 0 {
		summary += fmt.Sprintf(", %d permission(s) tightened", n)
	}
	return summary, nil
}

func (s *server) reload() (string, error) {
	var done []string
	for i := range s.srv.Targets {
		t := &s.srv.Targets[i]
		if !s.changed[t.ID] || t.Reload == "" {
			continue
		}
		s.reloaded = append(s.reloaded, t.ID)
		if err := s.runLogged(t.ID, t.Reload, t); err != nil {
			return "", fmt.Errorf("%s: %w", t.ID, err)
		}
		done = append(done, t.ID)
	}
	if s.srv.Post != "" {
		s.reloaded = append(s.reloaded, postID)
		if err := s.runLogged(postID, s.srv.Post, nil); err != nil {
			return "", fmt.Errorf("%s: %w", postID, err)
		}
		done = append(done, postID)
	}
	if len(done) == 0 {
		return "nothing to reload", nil
	}
	return strings.Join(done, ", "), nil
}

const postID = "POST"

func (s *server) runLogged(label, cmd string, t *inventory.Target) error {
	out, err := s.exec(cmd, t, s.srv.Timeout)
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line != "" {
			s.log.Logf("    %s | %s", label, line)
		}
	}
	return err
}

func (s *server) verify() (string, error) {
	for _, addr := range s.srv.Verify {
		c, err := tlsprobe.WaitFor(addr, s.bundle.Leaf, 10, 3*time.Second)
		if err != nil {
			return "", fmt.Errorf("%s: %w%s", addr, err, served(c))
		}
		s.log.Logf("  ✓ %-44s serves the NEW certificate (expires %s)", addr, c.NotAfter.Format(time.DateOnly))
	}
	return fmt.Sprintf("%d port(s) serve the new certificate", len(s.srv.Verify)), nil
}

func served(c *x509.Certificate) string {
	if c == nil {
		return ""
	}
	return fmt.Sprintf(" (serving %s, expires %s)", c.Subject.CommonName, c.NotAfter.Format(time.DateOnly))
}

// rollback restores the old state after a failure past the replace step.
func (s *server) rollback(cause error) (Status, error) {
	if len(s.replaced)+len(s.created)+len(s.reloaded)+len(s.fixedModes) == 0 {
		return StatusRolledBack, fmt.Errorf("%w (nothing had been changed)", cause)
	}
	s.log.Logf("↺ rolling back")
	var failed []error
	for _, dst := range s.replaced {
		bak := dst + ".bak-" + s.Stamp
		if _, err := s.exec(fmt.Sprintf(`cat %[1]s > %[2]s && rm -f %[1]s`, remote.Shq(bak), remote.Shq(dst)), nil, 0); err != nil {
			failed = append(failed, fmt.Errorf("restore %s: %w", dst, err))
			continue
		}
		s.log.Logf("  ↺ %s restored", dst)
	}
	// Content restore writes into the file and keeps its current (tightened)
	// mode, so put the old mode back explicitly: rollback means "as before".
	for _, dst := range s.fixedModes {
		if _, err := s.exec("chmod "+s.modeOld[dst]+" "+remote.Shq(dst), nil, 0); err != nil {
			failed = append(failed, fmt.Errorf("restore mode of %s: %w", dst, err))
		}
	}
	for _, dst := range s.created {
		if _, err := s.exec("rm -f "+remote.Shq(dst), nil, 0); err != nil {
			failed = append(failed, fmt.Errorf("remove %s: %w", dst, err))
			continue
		}
		s.log.Logf("  ↺ %s removed", dst)
	}
	// Every service that was reloaded must pick the old certificate up again.
	for _, id := range s.reloaded {
		label, cmd, t := s.undo(id)
		if cmd == "" {
			continue
		}
		if err := s.runLogged(label, cmd, t); err != nil {
			failed = append(failed, fmt.Errorf("%s: %w", label, err))
		}
	}
	if len(failed) > 0 {
		return StatusRollbackFailed, fmt.Errorf("%w; ROLLBACK INCOMPLETE: %w — backups are <file>.bak-%s", cause, errors.Join(failed...), s.Stamp)
	}
	return StatusRolledBack, fmt.Errorf("%w — old certificate restored", cause)
}

// undo returns what to run to revert a reload: the explicit rollback, or the
// reload again (with the old files back in place).
func (s *server) undo(id string) (label, cmd string, t *inventory.Target) {
	if id == postID {
		return "ROLLBACK", s.srv.Rollback, nil
	}
	for i := range s.srv.Targets {
		if t = &s.srv.Targets[i]; t.ID == id {
			break
		}
	}
	cmd = t.Rollback
	if cmd == "" && len(s.replaced) > 0 {
		cmd = t.Reload
	}
	return id + " rollback", cmd, t
}

func (s *server) dropBackups() {
	if s.srv.Backup || len(s.replaced) == 0 {
		return
	}
	var baks []string
	for _, dst := range s.replaced {
		baks = append(baks, remote.Shq(dst+".bak-"+s.Stamp))
	}
	if _, err := s.exec("rm -f "+strings.Join(baks, " "), nil, 0); err != nil {
		s.log.Logf("! could not remove backups: %v", err)
	}
}

func (s *server) cleanupTmp() {
	if len(s.staged) == 0 {
		return
	}
	dir := filepath.Dir(s.staged[0].tmp)
	if _, err := s.client.Run("rm -rf "+remote.Shq(dir), remote.RunOpts{}); err != nil {
		s.log.Logf("! could not remove %s: %v", dir, err)
	}
}

func (s *server) plan() {
	srv := s.srv
	s.log.Logf("connect  %s@%s:%d  sudo=%v  backup=%v  timeout=%s", srv.User, srv.Host, srv.Port, srv.Sudo, srv.Backup, srv.Timeout)
	for _, t := range srv.Targets {
		s.log.Logf("target   %s (%s)", t.ID, t.Type)
		for _, f := range t.Files {
			note := ""
			if f.Create {
				note = ", created if missing"
			}
			s.log.Logf("  file   %s ← %s%s", f.Dest, f.Kind, note)
		}
		for _, h := range t.Health {
			s.log.Logf("  health %s", oneLine(h))
		}
		if t.Reload != "" {
			s.log.Logf("  reload %s", oneLine(t.Reload))
		}
		if t.Rollback != "" {
			s.log.Logf("  undo   %s", oneLine(t.Rollback))
		}
		for _, k := range sortedKeys(t.Env) {
			if v := t.Env[k]; v != "" && k != "CERT" && k != "KEY" {
				s.log.Logf("  %-14s %s", strings.ToLower(k), v)
			}
		}
	}
	if srv.Health != "" {
		s.log.Logf("health   %s", oneLine(srv.Health))
	}
	if srv.Post != "" {
		s.log.Logf("post     %s", oneLine(srv.Post))
	}
	for _, v := range srv.Verify {
		s.log.Logf("verify   %s must serve the new certificate", v)
	}
}

func oneLine(s string) string {
	var lines []string
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") && !strings.HasPrefix(l, "set -") {
			lines = append(lines, l)
		}
	}
	switch len(lines) {
	case 0:
		return ""
	case 1:
		return lines[0]
	}
	return fmt.Sprintf("%s … (+%d lines)", lines[0], len(lines)-1)
}

func sortedKeys(m map[string]string) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
