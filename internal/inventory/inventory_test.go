package inventory

import (
	"maps"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ilramdhan/sslsync/internal/certs"
)

// base is a minimal valid configuration; tests copy and modify it.
var base = map[string]string{
	"SSLSYNC_SERVERS":   "WEB_PROD, k8s-staging",
	"DEFAULT_CERT_FILE": "certs/x/fullchain.crt",
	"DEFAULT_KEY_FILE":  "certs/x/x.key",
	"DEFAULT_PASSWORD":  "shared",

	"WEB_PROD_GROUP":       "Prod",
	"WEB_PROD_HOST":        "web.example.com",
	"WEB_PROD_USER":        "ubuntu",
	"WEB_PROD_TARGETS":     "nginx, api",
	"WEB_PROD_NGINX_CERT":  "/etc/ssl/web.crt",
	"WEB_PROD_NGINX_KEY":   "/etc/ssl/web.key",
	"WEB_PROD_API_TYPE":    "minio",
	"WEB_PROD_API_CERT":    "/mnt/minio/certs/public.crt",
	"WEB_PROD_API_KEY":     "/mnt/minio/certs/private.key",
	"WEB_PROD_API_SERVICE": "minio-api",
	"WEB_PROD_VERIFY":      ":443, files.example.com:9000",

	"K8S_STAGING_HOST":            "10.0.0.5",
	"K8S_STAGING_PORT":            "2222",
	"K8S_STAGING_USER":            "root",
	"K8S_STAGING_TARGETS":         "k8s",
	"K8S_STAGING_K8S_CERT":        "/root/ssl/tls.crt",
	"K8S_STAGING_K8S_KEY":         "/root/ssl/tls.key",
	"K8S_STAGING_K8S_TLS_SECRETS": "app-tls",
	"K8S_STAGING_TIMEOUT":         "15m",
}

func with(changes map[string]string) map[string]string {
	m := maps.Clone(base)
	for k, v := range changes {
		if v == "" {
			delete(m, k)
		} else {
			m[k] = v
		}
	}
	return m
}

func mustLoad(t *testing.T, vars map[string]string) *Inventory {
	t.Helper()
	inv, err := Load(vars, slices.Collect(maps.Keys(vars)))
	if err != nil {
		t.Fatal(err)
	}
	return inv
}

func TestLoad(t *testing.T) {
	inv := mustLoad(t, base)
	if len(inv.Servers) != 2 {
		t.Fatalf("got %d servers", len(inv.Servers))
	}
	if len(inv.Warnings) != 0 {
		t.Errorf("unexpected warnings: %v", inv.Warnings)
	}

	web := inv.Servers[0]
	if web.Name != "web-prod" || web.Group != "prod" || web.Port != 22 || !web.Sudo || web.Password != "shared" || !web.Backup {
		t.Errorf("web defaults wrong: %+v", web)
	}
	if web.CertFile != "certs/x/fullchain.crt" {
		t.Errorf("DEFAULT_CERT_FILE not applied: %q", web.CertFile)
	}
	if want := []string{"web.example.com:443", "files.example.com:9000"}; !slices.Equal(web.Verify, want) {
		t.Errorf("Verify = %v, want %v", web.Verify, want)
	}
	api := web.Targets[1]
	if api.Type != "minio" || api.Env["SERVICE"] != "minio-api" || !strings.Contains(api.Reload, "$SERVICE") {
		t.Errorf("minio target wrong: %+v", api)
	}
	if api.Files[1].Kind != certs.Key || api.Files[1].Mode() != "0600" || api.Files[0].Mode() != "0644" {
		t.Errorf("minio files wrong: %+v", api.Files)
	}

	k8s := inv.Servers[1]
	if k8s.Key != "K8S_STAGING" || k8s.Port != 2222 || k8s.Sudo || k8s.Timeout != 15*time.Minute {
		t.Errorf("k8s server wrong: %+v", k8s)
	}
	kt := k8s.Targets[0]
	if !kt.Files[0].Create || kt.Env["TLS_SECRETS"] != "app-tls" || kt.Env["KUBECTL"] != "kubectl" || kt.Rollback == "" || kt.State == "" {
		t.Errorf("k8s target wrong: %+v", kt)
	}
	if kt.Env["CERT"] != "/root/ssl/tls.crt" {
		t.Errorf("CERT not exported: %v", kt.Env)
	}
}

func TestOverrides(t *testing.T) {
	inv := mustLoad(t, with(map[string]string{
		"WEB_PROD_NGINX_RELOAD":       "systemctl restart nginx",
		"WEB_PROD_NGINX_HEALTH":       "none",
		"WEB_PROD_NGINX_CERT_CONTENT": "leaf",
		"WEB_PROD_SUDO":               "no",
	}))
	n := inv.Servers[0].Targets[0]
	if n.Reload != "systemctl restart nginx" || n.Files[0].Kind != certs.Leaf || inv.Servers[0].Sudo {
		t.Errorf("overrides not applied: %+v", n)
	}
	if len(n.Health) != 0 {
		t.Errorf(`HEALTH=none should disable the default checks, got %v`, n.Health)
	}
	inv = mustLoad(t, with(map[string]string{"WEB_PROD_NGINX_RELOAD": "none"}))
	if inv.Servers[0].Targets[0].Reload != "" {
		t.Error(`RELOAD=none should disable the reload`)
	}
}

func TestWarnsAboutUnusedVariables(t *testing.T) {
	inv := mustLoad(t, with(map[string]string{
		"WEB_PROD_NGINX_CRT":     "/typo",            // misspelled setting
		"WEB_PROD_POSTGRES_CERT": "/x",               // target not in TARGETS
		"OLD_SERVER_HOST":        "gone.example.com", // server not in SSLSYNC_SERVERS
	}))
	got := strings.Join(inv.Warnings, "\n")
	for _, want := range []string{"WEB_PROD_NGINX_CRT", "WEB_PROD_POSTGRES_CERT", "OLD_SERVER_HOST is not used"} {
		if !strings.Contains(got, want) {
			t.Errorf("no warning for %s in:\n%s", want, got)
		}
	}
}

func TestLoadErrors(t *testing.T) {
	cases := map[string]struct {
		changes map[string]string
		want    string
	}{
		"no servers":         {map[string]string{"SSLSYNC_SERVERS": ""}, "SSLSYNC_SERVERS is empty"},
		"bad name":           {map[string]string{"SSLSYNC_SERVERS": "WEB__PROD"}, "single underscores"},
		"reserved name":      {map[string]string{"SSLSYNC_SERVERS": "DEFAULT"}, "reserved"},
		"listed twice":       {map[string]string{"SSLSYNC_SERVERS": "WEB_PROD,web-prod"}, "listed twice"},
		"missing host":       {map[string]string{"WEB_PROD_HOST": ""}, "WEB_PROD_HOST is required"},
		"no auth":            {map[string]string{"DEFAULT_PASSWORD": ""}, "set WEB_PROD_PASSWORD or WEB_PROD_SSH_KEY"},
		"no targets":         {map[string]string{"WEB_PROD_TARGETS": ""}, "WEB_PROD_TARGETS is required"},
		"unknown type":       {map[string]string{"WEB_PROD_API_TYPE": "minoi"}, `unknown type "minoi"`},
		"missing path":       {map[string]string{"WEB_PROD_NGINX_KEY": ""}, "WEB_PROD_NGINX_KEY is required"},
		"relative path":      {map[string]string{"WEB_PROD_NGINX_KEY": "etc/web.key"}, "must be absolute"},
		"same path twice":    {map[string]string{"WEB_PROD_API_KEY": "/etc/ssl/web.key"}, "used by two targets"},
		"bad port":           {map[string]string{"K8S_STAGING_PORT": "22x"}, "must be a port number"},
		"bad bool":           {map[string]string{"WEB_PROD_SUDO": "maybe"}, "true or false"},
		"bad duration":       {map[string]string{"K8S_STAGING_TIMEOUT": "10"}, "duration"},
		"bad verify":         {map[string]string{"WEB_PROD_VERIFY": "web.example.com"}, "host:port"},
		"bad content":        {map[string]string{"WEB_PROD_NGINX_CERT_CONTENT": "crt"}, "must be fullchain"},
		"k8s without secret": {map[string]string{"K8S_STAGING_K8S_TLS_SECRETS": ""}, "TLS_SECRETS is required"},
		"same login twice": {map[string]string{
			"K8S_STAGING_HOST": "web.example.com", "K8S_STAGING_PORT": "", "K8S_STAGING_USER": "ubuntu",
		}, "same login"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			vars := with(tc.changes)
			_, err := Load(vars, nil)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got: %v", tc.want, err)
			}
		})
	}
}

func TestSelect(t *testing.T) {
	inv := mustLoad(t, base)
	names := func(only, exclude []string, group string) string {
		s, err := inv.Select(only, exclude, group)
		if err != nil {
			return "error"
		}
		var out []string
		for _, x := range s {
			out = append(out, x.Name)
		}
		return strings.Join(out, ",")
	}
	for _, tc := range []struct {
		only, exclude []string
		group, want   string
	}{
		{nil, nil, "", "web-prod,k8s-staging"},
		{[]string{"web-prod"}, nil, "", "web-prod"},
		{[]string{"K8S_STAGING"}, nil, "", "k8s-staging"},
		{nil, []string{"web-prod"}, "", "k8s-staging"},
		{nil, nil, "PROD", "web-prod"},
		{[]string{"nope"}, nil, "", "error"},
		{nil, nil, "qa", "error"},
		{[]string{"web-prod"}, []string{"web-prod"}, "", "error"},
	} {
		if got := names(tc.only, tc.exclude, tc.group); got != tc.want {
			t.Errorf("Select(%v, %v, %q) = %s, want %s", tc.only, tc.exclude, tc.group, got, tc.want)
		}
	}
}

func TestKeyMode(t *testing.T) {
	inv := mustLoad(t, base)
	nginxKey := inv.Servers[0].Targets[0].Files[1]
	k8sKey := inv.Servers[1].Targets[0].Files[1]
	k8sCert := inv.Servers[1].Targets[0].Files[0]

	cases := []struct {
		name string
		f    File
		cur  os.FileMode
		want string
	}{
		{"world-readable key is tightened", nginxKey, 0o644, "0640"},
		{"group-readable key is kept (e.g. ssl-cert group)", nginxKey, 0o640, ""},
		{"private key untouched", nginxKey, 0o600, ""},
		{"k8s key enforced to 0600", k8sKey, 0o644, "0600"},
		{"k8s key already 0600", k8sKey, 0o600, ""},
		{"certificates are never changed", k8sCert, 0o644, ""},
	}
	for _, tc := range cases {
		if got := tc.f.SecureMode(tc.cur); got != tc.want {
			t.Errorf("%s: SecureMode(%o) = %q, want %q", tc.name, tc.cur, got, tc.want)
		}
	}
	if k8sKey.Mode() != "0600" || nginxKey.Mode() != "0600" || k8sCert.Mode() != "0644" {
		t.Errorf("create modes wrong: %s %s %s", k8sKey.Mode(), nginxKey.Mode(), k8sCert.Mode())
	}

	for _, bad := range []string{"0644", "0777", "abc", "1777"} {
		if _, err := Load(with(map[string]string{"WEB_PROD_NGINX_KEY_MODE": bad}), nil); err == nil {
			t.Errorf("KEY_MODE=%s should be rejected", bad)
		}
	}
	if _, err := Load(with(map[string]string{"WEB_PROD_NGINX_KEY_MODE": "0640"}), nil); err != nil {
		t.Errorf("KEY_MODE=0640: %v", err)
	}
}
