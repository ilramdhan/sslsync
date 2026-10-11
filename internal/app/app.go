// Package app implements the sslsync commands on top of the inventory,
// certs, runner and report packages.
package app

import (
	"bufio"
	_ "embed"
	"encoding/base64"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/joho/godotenv"

	"github.com/ilramdhan/sslsync/internal/certs"
	"github.com/ilramdhan/sslsync/internal/inventory"
	"github.com/ilramdhan/sslsync/internal/report"
	"github.com/ilramdhan/sslsync/internal/runner"
	"github.com/ilramdhan/sslsync/internal/tlsprobe"
)

const Usage = `usage: sslsync <command> [flags]

Start:
  init          create .env (from the documented template), certs/ and .gitignore here

Discover (read-only, uses your ssh client, asks for passwords):
  find USER@HOST [DOMAIN]   where a server keeps certificates, which ports serve TLS
  trust                     add unknown server host keys to known_hosts (shows fingerprint, asks)

Inspect (read-only):
  validate      local certificate files: expiry, chain, key match, SAN covers every host
  list          servers and targets as configured in .env
  status        NEW/OLD certificate on every VERIFY port (no SSH)
  plan          what deploy would do, per server (no connection)
  check         SSH pre-flight: connect, sudo, health, files, secrets, current TLS
  targets       built-in target types and their settings
  known-hosts   servers whose SSH host key is not in known_hosts
  version       print the version

Change:
  deploy        check → upload → replace → reload → health → verify, rollback on failure

Flags:
`

//go:embed env.example
var envExample []byte

//go:embed find-ssl.sh
var findScript string

// Version is set at build time: -ldflags "-X .../internal/app.Version=v1.2.3".
var Version = "dev"

// ErrFailed is returned when a command ran but some servers failed; the
// details have been printed already.
var ErrFailed = errors.New("failed")

type options struct {
	envFiles             []string
	only, exclude, group string
	logDir               string
	parallel, retries    int
	yes                  bool
	out                  io.Writer
}

// Main runs one command and returns the process exit code.
func Main(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") || args[0] == "help" {
		fmt.Fprint(stderr, Usage)
		newFlags(&options{}).PrintDefaults()
		return 2
	}
	cmd := args[0]
	o := options{out: stdout}
	fs := newFlags(&o)
	if err := fs.Parse(args[1:]); err != nil {
		return 2
	}
	switch cmd {
	case "version":
		fmt.Fprintln(stdout, "sslsync", Version)
		return 0
	case "targets":
		fmt.Fprint(stdout, inventory.TypeHelp())
		return 0
	case "find":
		if err := find(fs.Args()); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	case "init":
		if err := initDir(stdout); err != nil {
			fmt.Fprintf(stderr, "error: %v\n", err)
			return 1
		}
		return 0
	}

	if err := run(cmd, o); err != nil {
		if !errors.Is(err, ErrFailed) {
			fmt.Fprintf(stderr, "error: %v\n", err)
		}
		return 1
	}
	return 0
}

func newFlags(o *options) *flag.FlagSet {
	fs := flag.NewFlagSet("sslsync", flag.ContinueOnError)
	fs.Func("env", "env file to load, repeatable; later files win (default .env)", func(v string) error {
		o.envFiles = append(o.envFiles, v)
		return nil
	})
	fs.StringVar(&o.only, "only", "", "comma-separated server names (WEB_PROD or web-prod)")
	fs.StringVar(&o.exclude, "exclude", "", "comma-separated server names to skip")
	fs.StringVar(&o.group, "group", "", "only servers with this <SERVER>_GROUP")
	fs.StringVar(&o.logDir, "log-dir", "logs", "where run logs go (empty = none)")
	fs.IntVar(&o.parallel, "parallel", 4, "servers processed at the same time")
	fs.IntVar(&o.retries, "retries", 3, "SSH connection attempts on network errors")
	fs.BoolVar(&o.yes, "yes", false, "deploy without the confirmation prompt")
	return fs
}

func run(cmd string, o options) error {
	inv, err := loadInventory(o.envFiles)
	if err != nil {
		return err
	}
	for _, w := range inv.Warnings {
		fmt.Fprintf(o.out, "⚠ %s\n", w)
	}
	servers, err := inv.Select(splitList(o.only), splitList(o.exclude), o.group)
	if err != nil {
		return err
	}

	switch cmd {
	case "list":
		printList(o.out, servers)
		return nil
	case "known-hosts":
		return printUnknownHosts(o.out, inv.KnownHosts, servers)
	case "trust":
		return trust(inv.KnownHosts, servers)
	}

	bundles, err := loadBundles(servers)
	if err != nil {
		return err
	}
	switch cmd {
	case "validate":
		printBundles(func(f string, a ...any) { fmt.Fprintf(o.out, f, a...) }, bundles)
		fmt.Fprintf(o.out, "\n✓ certificate files valid for %d server(s)\n", len(servers))
		return nil
	case "status":
		return status(o.out, servers, bundles)
	case "plan":
		return execute(runner.ModePlan, o, inv, servers, bundles)
	case "check":
		return execute(runner.ModeCheck, o, inv, servers, bundles)
	case "deploy":
		return execute(runner.ModeDeploy, o, inv, servers, bundles)
	}
	return fmt.Errorf("unknown command %q (run `sslsync help`)", cmd)
}

// loadInventory reads the env files and builds the inventory. Variables
// already set in the process environment win over the files, so a value can
// be overridden for one run: WEB_PROD_PORT=2222 make check.
func loadInventory(files []string) (*inventory.Inventory, error) {
	if len(files) == 0 {
		files = []string{".env"}
	}
	vars := map[string]string{}
	var fileKeys []string
	for _, f := range files {
		m, err := godotenv.Read(f)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w (copy .env.example to .env)", f, err)
		}
		if err := checkPermissions(f); err != nil {
			return nil, err
		}
		for k, v := range m {
			vars[k] = v
			fileKeys = append(fileKeys, k)
		}
	}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok && v != "" && isConfigKey(k, vars) {
			vars[k] = v
		}
	}
	slices.Sort(fileKeys)
	return inventory.Load(vars, slices.Compact(fileKeys))
}

// isConfigKey limits environment overrides to names the files use or the
// tool owns, so unrelated variables (USER, HOME, …) never leak in.
func isConfigKey(k string, fileVars map[string]string) bool {
	if _, ok := fileVars[k]; ok {
		return true
	}
	return strings.HasPrefix(k, "SSLSYNC_") || strings.HasPrefix(k, inventory.DefaultPrefix)
}

// checkPermissions refuses an env file other users can read: it holds
// passwords.
func checkPermissions(path string) error {
	st, err := os.Stat(path)
	if err != nil {
		return err
	}
	if st.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("%s is readable by other users (mode %o) — run: chmod 600 %s", path, st.Mode().Perm(), path)
	}
	return nil
}

// loadBundles loads every distinct certificate pair once and checks it
// covers the VERIFY hosts of the servers that use it.
func loadBundles(servers []inventory.Server) (map[string]*certs.Bundle, error) {
	bundles := map[string]*certs.Bundle{}
	var errs []string
	for i := range servers {
		s := &servers[i]
		key := runner.BundleKey(s)
		b, seen := bundles[key]
		if !seen {
			var err error
			b, err = certs.Load(s.CertFile, s.KeyFile)
			if err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", s.Name, err))
				bundles[key] = nil
				continue
			}
			bundles[key] = b
		}
		if b == nil {
			continue
		}
		if err := b.CheckHosts(s.VerifyHosts()); err != nil {
			errs = append(errs, fmt.Sprintf("%s: %v", s.Name, err))
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("certificate check failed — nothing was done:\n  %s", strings.Join(slices.Compact(errs), "\n  "))
	}
	return bundles, nil
}

func printBundles(printf func(string, ...any), bundles map[string]*certs.Bundle) {
	keys := make([]string, 0, len(bundles))
	for k := range bundles {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b := bundles[k]
		printf("  cert  %s\n", b.CertFile)
		if b.KeyFile != "" {
			printf("  key   %s\n", b.KeyFile)
		}
		printf("        %s\n", certs.Describe(b.Leaf))
		for _, c := range b.Chain {
			printf("        + chain: %s (expires %s)\n", c.Subject.CommonName, c.NotAfter.Format(time.DateOnly))
		}
		for _, warn := range b.Warnings {
			printf("  ⚠ %s\n", warn)
		}
	}
}

// ------------------------------------------------------------------ list

func printList(w io.Writer, servers []inventory.Server) {
	for _, s := range servers {
		auth := "password"
		if s.SSHKey != "" {
			auth = "key " + s.SSHKey
		}
		fmt.Fprintf(w, "%-18s group=%-8s %s@%s:%d  (%s, sudo=%v)\n", s.Name, s.Group, s.User, s.Host, s.Port, auth, s.Sudo)
		for _, t := range s.Targets {
			fmt.Fprintf(w, "    %-10s %s\n", t.ID, t.Type)
			for _, f := range t.Files {
				extra := ""
				if f.Create {
					extra = "  [create]"
				}
				fmt.Fprintf(w, "        %-9s → %s%s\n", f.Kind, f.Dest, extra)
			}
		}
		for _, v := range s.Verify {
			fmt.Fprintf(w, "    verify     %s\n", v)
		}
		fmt.Fprintln(w)
	}
}

func printUnknownHosts(w io.Writer, knownHosts string, servers []inventory.Server) error {
	data, err := os.ReadFile(expandHome(knownHosts))
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	known := string(data)
	for _, s := range servers {
		entry := s.Host
		if s.Port != 22 {
			entry = fmt.Sprintf("[%s]:%d", s.Host, s.Port)
		}
		if !strings.Contains(known, entry+" ") && !strings.Contains(known, entry+",") {
			fmt.Fprintf(w, "%s %d %s\n", s.Name, s.Port, s.Host)
		}
	}
	return nil
}

// ---------------------------------------------------------------- status

func status(w io.Writer, servers []inventory.Server, bundles map[string]*certs.Bundle) error {
	type row struct {
		server, addr, state, detail string
		order                       int
	}
	var (
		rows []row
		mu   sync.Mutex
		wg   sync.WaitGroup
	)
	for i := range servers {
		s := &servers[i]
		want := bundles[runner.BundleKey(s)].Leaf
		for _, addr := range s.Verify {
			wg.Go(func() {
				r := row{server: s.Name, addr: addr, order: i}
				c, err := tlsprobe.Fetch(addr)
				switch {
				case err != nil:
					r.state, r.detail = "ERROR", err.Error()
				case c.Equal(want):
					r.state = "NEW"
				default:
					r.state = "OLD"
				}
				if c != nil {
					r.detail = fmt.Sprintf("%s, expires %s (%dd)", c.Subject.CommonName, c.NotAfter.Format(time.DateOnly), int(time.Until(c.NotAfter).Hours()/24))
				}
				mu.Lock()
				rows = append(rows, r)
				mu.Unlock()
			})
		}
	}
	wg.Wait()
	sort.Slice(rows, func(i, j int) bool {
		if rows[i].order != rows[j].order {
			return rows[i].order < rows[j].order
		}
		return rows[i].addr < rows[j].addr
	})
	counts := map[string]int{}
	fmt.Fprintf(w, "%-18s %-40s %-6s %s\n", "SERVER", "ADDRESS", "STATE", "SERVED CERTIFICATE")
	for _, r := range rows {
		counts[r.state]++
		fmt.Fprintf(w, "%-18s %-40s %-6s %s\n", r.server, r.addr, r.state, r.detail)
	}
	fmt.Fprintf(w, "\n%d NEW, %d OLD, %d ERROR\n", counts["NEW"], counts["OLD"], counts["ERROR"])
	if counts["OLD"]+counts["ERROR"] > 0 {
		return ErrFailed
	}
	return nil
}

// ------------------------------------------------- plan / check / deploy

func execute(mode runner.Mode, o options, inv *inventory.Inventory, servers []inventory.Server, bundles map[string]*certs.Bundle) error {
	logRoot := o.logDir
	if mode == runner.ModePlan {
		logRoot = ""
	}
	rr, err := report.New(logRoot, mode.String())
	if err != nil {
		return fmt.Errorf("log: %w", err)
	}

	rr.Printf("== sslsync %s  %s\n\nCertificate:\n", mode, time.Now().Format(time.DateTime))
	printBundles(rr.Printf, bundles)
	rr.Printf("\nServers (%d):\n", len(servers))
	for _, s := range servers {
		rr.Printf("  - %-18s %-8s %s@%s:%d  %s\n", s.Name, s.Group, s.User, s.Host, s.Port, targetNames(s))
	}
	rr.Printf("\n")

	if mode == runner.ModeDeploy && !o.yes && !confirm(len(servers)) {
		rr.Printf("Aborted, nothing changed.\n")
		_ = rr.Finish(nil, nil)
		return ErrFailed
	}

	r := &runner.Runner{Bundles: bundles, Mode: mode, Stamp: rr.Stamp, Retries: o.retries, KnownHosts: expandHome(inv.KnownHosts)}
	results := make([]report.ServerResult, len(servers))
	sem := make(chan struct{}, max(1, o.parallel))
	var wg sync.WaitGroup
	for i := range servers {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			results[i] = r.Run(&servers[i], rr.Server(servers[i].Name))
		})
	}
	wg.Wait()

	failed := printSummary(rr, results)
	var certInfo map[string]any
	for _, b := range bundles {
		certInfo = map[string]any{"subject": b.Leaf.Subject.CommonName, "san": b.Leaf.DNSNames,
			"not_after": b.Leaf.NotAfter.Format(time.RFC3339), "sha256": certs.Fingerprint(b.Leaf), "file": b.CertFile}
	}
	if err := rr.Finish(certInfo, results); err != nil {
		fmt.Fprintf(o.out, "warning: writing logs: %v\n", err)
	}
	if rr.Dir != "" {
		fmt.Fprintf(o.out, "\nLogs: %s/  (run.log, <server>.log, summary.json)\n", rr.Dir)
	}
	if len(failed) > 0 {
		fmt.Fprintf(o.out, "Re-run only the failed ones:  make %s ONLY=%s\n", mode, strings.Join(failed, ","))
		return ErrFailed
	}
	return nil
}

func confirm(n int) bool {
	fmt.Printf("Deploy the certificate above to these %d server(s)? Type 'yes' to continue: ", n)
	ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(ans) == "yes"
}

func printSummary(rr *report.Run, results []report.ServerResult) (failed []string) {
	rr.Printf("\n================================ SUMMARY ================================\n")
	rr.Printf("%-18s %-8s %-17s %-9s %s\n", "SERVER", "GROUP", "STATUS", "TIME", "DETAIL")
	for _, res := range results {
		mark := "✓"
		if !res.OK {
			mark = "✗"
			failed = append(failed, res.Server)
		}
		detail := res.Error
		if detail == "" && len(res.Changes) > 0 {
			detail = fmt.Sprintf("%d file(s) changed", len(res.Changes))
		}
		rr.Printf("%-18s %-8s %s %-15s %-9s %s\n", res.Server, res.Group, mark, res.Status, res.Duration, detail)
	}
	rr.Printf("\n%d ok, %d failed\n", len(results)-len(failed), len(failed))
	return failed
}

// ---------------------------------------------------------------- helpers

func targetNames(s inventory.Server) string {
	var names []string
	for _, t := range s.Targets {
		names = append(names, strings.ToLower(t.ID))
	}
	return "[" + strings.Join(names, ",") + "]"
}

func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' })
}

func expandHome(p string) string {
	if rest, ok := strings.CutPrefix(p, "~/"); ok {
		if home, err := os.UserHomeDir(); err == nil {
			return home + "/" + rest
		}
	}
	return p
}

// initDir prepares the current directory for a first run: a .env from the
// documented template (never overwritten), a certs/ folder, and a .gitignore
// that keeps secrets out of git if the folder is ever committed.
func initDir(w io.Writer) error {
	create := func(name string, data []byte, mode os.FileMode) error {
		f, err := os.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
		if errors.Is(err, os.ErrExist) {
			fmt.Fprintf(w, "  = %s already exists, left unchanged\n", name)
			return nil
		}
		if err != nil {
			return err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return err
		}
		fmt.Fprintf(w, "  + %s\n", name)
		return f.Close()
	}
	if err := create(".env", envExample, 0o600); err != nil {
		return err
	}
	if err := os.MkdirAll("certs", 0o700); err != nil {
		return err
	}
	fmt.Fprintln(w, "  + certs/")
	if err := create(".gitignore", []byte(".env\n.env.*\ncerts/\nlogs/\n"), 0o644); err != nil {
		return err
	}
	fmt.Fprint(w, `
Next steps:
  1. Put your certificate in certs/   (full chain + private key)
  2. Edit .env                        (servers, paths — every setting is explained inside)
  3. sslsync list                     (shows how .env was read, warns about typos)
  4. sslsync validate                 (checks the certificate)
  5. sslsync check                    (read-only check of every server)
`)
	return nil
}

// find runs the discovery script on a server through the system ssh client,
// so the operator's own ssh config, agent and password prompts all work.
func find(args []string) error {
	if len(args) == 0 {
		return errors.New("usage: sslsync find USER@HOST [DOMAIN] [-p PORT]")
	}
	target, domain, port := args[0], "", "22"
	for i := 1; i < len(args); i++ {
		if args[i] == "-p" && i+1 < len(args) {
			port = args[i+1]
			i++
			continue
		}
		domain = args[i]
	}
	// ssh -t keeps a terminal for the sudo prompt; the script goes as an
	// argument (base64, so no quoting issues) and is never written to disk.
	enc := base64.StdEncoding.EncodeToString([]byte(findScript))
	remote := fmt.Sprintf(`sudo bash -c "$(echo %s | base64 -d)" find-ssl %s`, enc, shellQuote(domain))
	cmd := exec.Command("ssh", "-t", "-p", port, target, remote)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	fmt.Fprintf(os.Stderr, "Connecting to %s — you may be asked for the SSH password, then the sudo password.\n", target)
	return cmd.Run()
}

// trust shows each unknown host key's fingerprint and appends it to
// known_hosts only when the operator confirms.
func trust(knownHosts string, servers []inventory.Server) error {
	var buf strings.Builder
	if err := printUnknownHosts(&buf, knownHosts, servers); err != nil {
		return err
	}
	in := bufio.NewReader(os.Stdin)
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		f := strings.Fields(line)
		if len(f) != 3 {
			continue
		}
		name, port, host := f[0], f[1], f[2]
		out, err := exec.Command("ssh-keyscan", "-p", port, "-t", "ed25519,ecdsa,rsa", host).Output()
		if err != nil || len(out) == 0 {
			fmt.Printf("%s (%s:%s): could not read the host key: %v\n", name, host, port, err)
			continue
		}
		fp := exec.Command("ssh-keygen", "-lf", "-")
		fp.Stdin = strings.NewReader(string(out))
		prints, _ := fp.Output()
		fmt.Printf("\n%s (%s:%s) presents:\n%s", name, host, port, prints)
		fmt.Print("Compare with the server's console (ssh-keygen -lf /etc/ssh/ssh_host_*_key.pub). Trust it? [y/N] ")
		ans, _ := in.ReadString('\n')
		if strings.TrimSpace(strings.ToLower(ans)) != "y" {
			fmt.Println("skipped")
			continue
		}
		f2, err := os.OpenFile(expandHome(knownHosts), os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
		if err != nil {
			return err
		}
		_, werr := f2.Write(out)
		if err := errors.Join(werr, f2.Close()); err != nil {
			return err
		}
		fmt.Println("saved")
	}
	if buf.Len() == 0 {
		fmt.Println("every server's host key is already in", knownHosts)
	}
	return nil
}

func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
