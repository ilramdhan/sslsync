// Package inventory builds the list of servers from environment variables
// (usually the .env file). There is no other config file: everything about a
// server — address, credentials, what runs on it and where its certificate
// files live — is a variable named <SERVER>_<SETTING>.
//
//	SSLSYNC_SERVERS=WEB_PROD,K8S_PROD
//	WEB_PROD_HOST=web.example.com
//	WEB_PROD_TARGETS=nginx,minio
//	WEB_PROD_NGINX_CERT=/etc/ssl/certs/ssl-bundle.crt
//	DEFAULT_PORT=22                      # fallback for every server
//
// See .env.example for every setting.
package inventory

import (
	"fmt"
	"net"
	"os"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/ilramdhan/sslsync/internal/certs"
)

const (
	ServersKey    = "SSLSYNC_SERVERS"
	KnownHostsKey = "SSLSYNC_KNOWN_HOSTS"
	DefaultPrefix = "DEFAULT_"
)

var (
	namePattern = regexp.MustCompile(`^[A-Z][A-Z0-9]*(_[A-Z0-9]+)*$`)
	reserved    = []string{"DEFAULT", "SSLSYNC"}
)

type Inventory struct {
	Servers    []Server
	KnownHosts string
	Warnings   []string // unused / misspelled variables and similar
}

type Server struct {
	Key   string // env prefix, e.g. WEB_PROD
	Name  string // display name, e.g. web-prod
	Group string

	Host string
	Port int
	User string

	Password         string
	SSHKey           string
	SSHKeyPassphrase string
	InsecureHostKey  bool

	Sudo         bool
	SudoPassword string

	CertFile string // local files to deploy
	KeyFile  string

	Targets []Target
	Verify  []string // host:port that must serve the new certificate

	Health   string // extra checks, run before and after
	Post     string // extra command after all target reloads
	Rollback string // undo for Post

	Backup  bool
	Timeout time.Duration // per reload / rollback command
}

// Target is one thing on a server that uses the certificate: a web server,
// MinIO, Kubernetes secrets, or plain files.
type Target struct {
	ID       string // as written in TARGETS, e.g. NGINX or API
	Type     string // nginx, apache, haproxy, minio, postgres, k8s, files
	Files    []File
	Health   []string
	Reload   string            // makes the service use the new files
	Rollback string            // runs after the old files are restored; empty = Reload again
	State    string            // optional: prints "OUTDATED <what>" for live state not yet updated
	Env      map[string]string // exported for Health/Reload/Rollback/State
}

// File is one destination file on the server.
type File struct {
	Target  string
	Dest    string
	Kind    certs.Kind
	Create  bool   // may be created when missing; otherwise missing = config error
	Owner   string // only when created
	KeyMode string // enforced mode of a key/pem file, e.g. "0600"; "" = keep, but never world-readable
}

// Mode is the mode a new file is created with. Existing files keep theirs,
// except that key files are tightened (see KeyMode).
func (f File) Mode() string {
	switch {
	case f.Kind.Secret() && f.KeyMode != "":
		return f.KeyMode
	case f.Kind.Secret():
		return "0600"
	}
	return "0644"
}

// SecureMode returns the mode a key file with the current mode cur must
// get, or "" when cur is fine. Certificates are public and never changed.
//
// With KeyMode set, that mode is enforced. Without it only permissions for
// "others" are removed, so a key shared with a service group (e.g. 0640
// root:ssl-cert for PostgreSQL) keeps working.
func (f File) SecureMode(cur os.FileMode) string {
	if !f.Kind.Secret() {
		return ""
	}
	want := cur &^ 0o007
	if f.KeyMode != "" {
		m, _ := strconv.ParseUint(f.KeyMode, 8, 32) // validated in buildTarget
		want = os.FileMode(m)
	}
	if want == cur.Perm() {
		return ""
	}
	return fmt.Sprintf("%04o", uint32(want))
}

// Files lists every destination file of the server.
func (s *Server) Files() []File {
	var out []File
	for _, t := range s.Targets {
		out = append(out, t.Files...)
	}
	return out
}

// VerifyHosts returns the distinct host names in Verify.
func (s *Server) VerifyHosts() []string {
	var hosts []string
	for _, v := range s.Verify {
		h, _, _ := net.SplitHostPort(v)
		if !slices.Contains(hosts, h) {
			hosts = append(hosts, h)
		}
	}
	return hosts
}

// Load builds the inventory. vars is every variable visible to the program;
// fileKeys are the ones that came from .env files and are checked for typos.
func Load(vars map[string]string, fileKeys []string) (*Inventory, error) {
	src := &source{vars: vars, used: map[string]bool{}}
	inv := &Inventory{KnownHosts: src.get(KnownHostsKey)}
	if inv.KnownHosts == "" {
		inv.KnownHosts = "~/.ssh/known_hosts"
	}

	keys, err := serverKeys(src.get(ServersKey))
	if err != nil {
		return nil, err
	}
	var errs []string
	for _, k := range keys {
		s, serr := loadServer(src, k)
		errs = append(errs, serr...)
		inv.Servers = append(inv.Servers, s)
	}
	errs = append(errs, checkDuplicates(inv.Servers)...)
	if len(errs) > 0 {
		return nil, fmt.Errorf("%d problem(s) in the configuration:\n  %s", len(errs), strings.Join(errs, "\n  "))
	}
	inv.Warnings = src.unused(fileKeys, keys)
	return inv, nil
}

func serverKeys(list string) ([]string, error) {
	if list == "" {
		return nil, fmt.Errorf("%s is empty — list your servers, e.g. %s=WEB_PROD,WEB_STAGING", ServersKey, ServersKey)
	}
	var keys, errs []string
	for _, k := range splitList(list) {
		k = normalizeKey(k)
		switch {
		case !namePattern.MatchString(k):
			errs = append(errs, fmt.Sprintf("%q: server names use letters, digits and single underscores, starting with a letter", k))
		case slices.Contains(reserved, k):
			errs = append(errs, fmt.Sprintf("%q is reserved", k))
		case slices.Contains(keys, k):
			errs = append(errs, fmt.Sprintf("%q is listed twice", k))
		default:
			keys = append(keys, k)
		}
	}
	if len(errs) > 0 {
		return nil, fmt.Errorf("%s: %s", ServersKey, strings.Join(errs, "; "))
	}
	return keys, nil
}

func loadServer(src *source, key string) (Server, []string) {
	f := fields{src: src, prefix: key}
	s := Server{Key: key, Name: displayName(key)}
	var errs []string
	fail := func(format string, args ...any) {
		errs = append(errs, s.Name+": "+fmt.Sprintf(format, args...))
	}

	s.Group = strings.ToLower(f.get("GROUP"))
	s.Host = f.get("HOST")
	s.User = f.get("USER")
	if s.Host == "" {
		fail("%s_HOST is required", key)
	}
	if s.User == "" {
		fail("%s_USER is required", key)
	}
	s.Port = f.int("PORT", 22, fail)
	s.Password = f.get("PASSWORD")
	s.SSHKey = f.get("SSH_KEY")
	s.SSHKeyPassphrase = f.get("SSH_KEY_PASSPHRASE")
	if s.Password == "" && s.SSHKey == "" {
		fail("set %s_PASSWORD or %s_SSH_KEY", key, key)
	}
	s.InsecureHostKey = f.bool("INSECURE_HOST_KEY", false, fail)
	s.Sudo = f.bool("SUDO", s.User != "root", fail)
	s.SudoPassword = f.get("SUDO_PASSWORD")
	if s.SudoPassword == "" {
		s.SudoPassword = s.Password
	}
	s.Backup = f.bool("BACKUP", true, fail)
	s.Timeout = f.duration("TIMEOUT", 10*time.Minute, fail)
	s.CertFile = f.get("CERT_FILE")
	s.KeyFile = f.get("KEY_FILE")
	if s.CertFile == "" {
		fail("set %s_CERT_FILE or %sCERT_FILE (local certificate file)", key, DefaultPrefix)
	}
	s.Health = f.get("HEALTH")
	s.Post = f.get("POST")
	s.Rollback = f.get("ROLLBACK")

	for _, v := range splitList(f.get("VERIFY")) {
		addr, err := verifyAddr(v, s.Host)
		if err != nil {
			fail("%s_VERIFY: %v", key, err)
			continue
		}
		s.Verify = append(s.Verify, addr)
	}

	ids := splitList(f.get("TARGETS"))
	if len(ids) == 0 {
		fail("%s_TARGETS is required (one or more of: %s)", key, strings.Join(TypeNames(), ", "))
	}
	for _, id := range ids {
		t, terr := buildTarget(f, normalizeKey(id))
		for _, e := range terr {
			fail("%s", e)
		}
		s.Targets = append(s.Targets, t)
	}
	if err := checkDests(s.Files()); err != nil {
		fail("%v", err)
	}
	return s, errs
}

// verifyAddr accepts "host:port" or ":port" (= the server's HOST).
func verifyAddr(v, host string) (string, error) {
	if strings.HasPrefix(v, ":") {
		v = host + v
	}
	h, p, err := net.SplitHostPort(v)
	if err != nil || h == "" {
		return "", fmt.Errorf("%q must be host:port or :port", v)
	}
	if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
		return "", fmt.Errorf("%q: invalid port", v)
	}
	return v, nil
}

func checkDests(files []File) error {
	seen := map[string]bool{}
	for _, f := range files {
		if !strings.HasPrefix(f.Dest, "/") {
			return fmt.Errorf("%s: path %q must be absolute", strings.ToLower(f.Target), f.Dest)
		}
		if seen[f.Dest] {
			return fmt.Errorf("%s is used by two targets", f.Dest)
		}
		seen[f.Dest] = true
	}
	return nil
}

func checkDuplicates(servers []Server) []string {
	var errs []string
	seen := map[string]string{}
	for _, s := range servers {
		id := fmt.Sprintf("%s@%s:%d", s.User, s.Host, s.Port)
		if other, ok := seen[id]; ok {
			errs = append(errs, fmt.Sprintf("%s and %s are the same login (%s) — merge their TARGETS", other, s.Name, id))
		}
		seen[id] = s.Name
	}
	return errs
}

// Select filters servers by name (WEB_PROD or web-prod) and group.
func (inv *Inventory) Select(only, exclude []string, group string) ([]Server, error) {
	known := map[string]bool{}
	groups := map[string]bool{}
	for _, s := range inv.Servers {
		known[s.Key] = true
		groups[s.Group] = true
	}
	norm := func(names []string) ([]string, error) {
		var out []string
		for _, n := range names {
			k := normalizeKey(n)
			if !known[k] {
				return nil, fmt.Errorf("unknown server %q (see `make list`)", n)
			}
			out = append(out, k)
		}
		return out, nil
	}
	onlyK, err := norm(only)
	if err != nil {
		return nil, err
	}
	excludeK, err := norm(exclude)
	if err != nil {
		return nil, err
	}
	group = strings.ToLower(group)
	if group != "" && !groups[group] {
		return nil, fmt.Errorf("unknown group %q", group)
	}

	var out []Server
	for _, s := range inv.Servers {
		if (len(onlyK) > 0 && !slices.Contains(onlyK, s.Key)) || slices.Contains(excludeK, s.Key) || (group != "" && s.Group != group) {
			continue
		}
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("no servers match the filter")
	}
	return out, nil
}

// ---------------------------------------------------------------- lookup

// source reads variables and remembers which were looked up, so unused
// (usually misspelled) ones can be reported.
type source struct {
	vars map[string]string
	used map[string]bool
}

func (s *source) get(key string) string {
	s.used[key] = true
	return strings.TrimSpace(s.vars[key])
}

func (s *source) unused(fileKeys, servers []string) []string {
	var warns []string
	for _, k := range fileKeys {
		if s.used[k] {
			continue
		}
		owner := ""
		for _, srv := range servers {
			if strings.HasPrefix(k, srv+"_") && len(srv) > len(owner) {
				owner = srv
			}
		}
		switch {
		case owner != "":
			warns = append(warns, fmt.Sprintf("%s is not a known setting of %s (typo? or the target is not in %s_TARGETS)", k, displayName(owner), owner))
		case strings.HasPrefix(k, DefaultPrefix):
			warns = append(warns, fmt.Sprintf("%s is not used by any server", k))
		default:
			warns = append(warns, fmt.Sprintf("%s is not used (server missing from %s?)", k, ServersKey))
		}
	}
	sort.Strings(warns)
	return warns
}

// fields reads <PREFIX>_<NAME>, falling back to DEFAULT_<NAME>.
type fields struct {
	src    *source
	prefix string
}

func (f fields) get(name string) string {
	if v := f.src.get(f.prefix + "_" + name); v != "" {
		return v
	}
	return f.src.get(DefaultPrefix + name)
}

func (f fields) bool(name string, def bool, fail func(string, ...any)) bool {
	v := strings.ToLower(f.get(name))
	switch v {
	case "":
		return def
	case "1", "true", "yes", "y", "on":
		return true
	case "0", "false", "no", "n", "off":
		return false
	}
	fail("%s_%s=%q must be true or false", f.prefix, name, v)
	return def
}

func (f fields) int(name string, def int, fail func(string, ...any)) int {
	v := f.get(name)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil || n < 1 || n > 65535 {
		fail("%s_%s=%q must be a port number", f.prefix, name, v)
		return def
	}
	return n
}

func (f fields) duration(name string, def time.Duration, fail func(string, ...any)) time.Duration {
	v := f.get(name)
	if v == "" {
		return def
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		fail("%s_%s=%q must be a duration like 90s or 10m", f.prefix, name, v)
		return def
	}
	return d
}

// splitList splits on commas, spaces and newlines.
func splitList(s string) []string {
	return strings.FieldsFunc(s, func(r rune) bool { return r == ',' || r == ' ' || r == '\n' || r == '\t' })
}

func normalizeKey(s string) string {
	return strings.ToUpper(strings.ReplaceAll(strings.TrimSpace(s), "-", "_"))
}

func displayName(key string) string {
	return strings.ToLower(strings.ReplaceAll(key, "_", "-"))
}
