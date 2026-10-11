package inventory

import (
	"maps"
	"net"
	"os"
	"slices"
	"strconv"
	"strings"
	"testing"
)

// FuzzLoad sets a handful of variables of the valid base configuration to
// arbitrary values. Load must never panic, and whatever it accepts must be
// safe to act on: real port numbers, absolute and distinct destination
// files, host:port verify addresses, positive timeouts, and key modes that
// never make a private key readable by every user.
func FuzzLoad(f *testing.F) {
	f.Add(base["SSLSYNC_SERVERS"], "22", base["WEB_PROD_TARGETS"], base["WEB_PROD_VERIFY"], base["WEB_PROD_NGINX_CERT"], "", "10m")
	f.Add("WEB_PROD", "65535", "nginx", ":443", "/etc/ssl/web.crt", "0640", "90s")
	f.Add("WEB_PROD", "0", "nginx,nginx", "[::1]:443,:8443", "/etc/ssl/web.key", "0644", "-1s")
	f.Add("web-prod,DEFAULT", "+22", "files", "host:+443", "relative.crt", "0o600", "0")
	f.Add("WEB_PROD", "99999", "nginx", "host:0443, :", "/", "777", "1h")
	f.Add("", "", "", "", "", "", "")

	f.Fuzz(func(t *testing.T, servers, port, targets, verify, nginxCert, keyMode, timeout string) {
		vars := maps.Clone(base)
		set := func(k, v string) {
			if v == "" {
				delete(vars, k)
			} else {
				vars[k] = v
			}
		}
		set("SSLSYNC_SERVERS", servers)
		set("WEB_PROD_PORT", port)
		set("WEB_PROD_TARGETS", targets)
		set("WEB_PROD_VERIFY", verify)
		set("WEB_PROD_NGINX_CERT", nginxCert)
		set("WEB_PROD_NGINX_KEY_MODE", keyMode)
		set("WEB_PROD_TIMEOUT", timeout)

		inv, err := Load(vars, slices.Collect(maps.Keys(vars)))
		if err != nil {
			return
		}
		for _, s := range inv.Servers {
			if s.Port < 1 || s.Port > 65535 {
				t.Errorf("%s: port %d", s.Name, s.Port)
			}
			if s.Timeout <= 0 {
				t.Errorf("%s: timeout %v", s.Name, s.Timeout)
			}
			seen := map[string]bool{}
			for _, file := range s.Files() {
				if !strings.HasPrefix(file.Dest, "/") {
					t.Errorf("%s: destination %q is not absolute", s.Name, file.Dest)
				}
				if seen[file.Dest] {
					t.Errorf("%s: destination %q used twice", s.Name, file.Dest)
				}
				seen[file.Dest] = true
				checkKeyMode(t, s.Name, file)
			}
			for _, v := range s.Verify {
				h, p, err := net.SplitHostPort(v)
				if err != nil || h == "" || p == "" || strings.Trim(p, "0123456789") != "" {
					t.Errorf("%s: verify address %q is not host:port", s.Name, v)
					continue
				}
				if n, _ := strconv.Atoi(p); n < 1 || n > 65535 {
					t.Errorf("%s: verify address %q has port %d", s.Name, v, n)
				}
			}
		}
	})
}

// checkKeyMode: a configured key mode, and every mode SecureMode asks for,
// must leave "other" without any permission.
func checkKeyMode(t *testing.T, server string, file File) {
	t.Helper()
	if file.KeyMode != "" {
		m, err := strconv.ParseUint(file.KeyMode, 8, 32)
		if err != nil || m > 0o777 || m&0o007 != 0 {
			t.Errorf("%s: key mode %q accepted", server, file.KeyMode)
		}
	}
	if !file.Kind.Secret() {
		return
	}
	for _, cur := range []os.FileMode{0o600, 0o640, 0o644, 0o666, 0o777, 0o007} {
		want := file.SecureMode(cur)
		if want == "" {
			want = strconv.FormatUint(uint64(cur.Perm()), 8)
		}
		m, err := strconv.ParseUint(want, 8, 32)
		if err != nil || m&0o007 != 0 {
			t.Errorf("%s: %s with mode %04o would become %q", server, file.Dest, cur, want)
		}
	}
}
