package inventory

import (
	_ "embed"
	"fmt"
	"slices"
	"sort"
	"strconv"
	"strings"

	"github.com/ilramdhan/sslsync/internal/certs"
)

// A target type knows which files a service needs, how to check it is
// healthy and how to make it load a new certificate. Every built-in default
// can be overridden per target with <SERVER>_<TARGET>_<SETTING>.
type targetType struct {
	desc     string
	files    []fileSpec
	health   []string
	reload   string
	rollback string
	state    string
	vars     []varSpec // extra settings exported to the scripts
	keyMode  string    // default <TARGET>_KEY_MODE
}

type fileSpec struct {
	setting  string     // CERT -> <SERVER>_<TARGET>_CERT
	kind     certs.Kind // default content
	required bool
	create   bool
}

type varSpec struct {
	setting, def string
	required     bool
}

//go:embed scripts/k8s-reload.sh
var k8sReload string

//go:embed scripts/k8s-rollback.sh
var k8sRollback string

//go:embed scripts/k8s-state.sh
var k8sState string

//go:embed scripts/k8s-health.sh
var k8sHealth string

var types = map[string]targetType{
	"nginx": {
		desc:   "Nginx: certificate + key files, `nginx -t` then reload",
		files:  []fileSpec{{"CERT", certs.FullChain, true, false}, {"KEY", certs.Key, true, false}},
		health: []string{"systemctl is-active --quiet nginx", "nginx -t -q"},
		reload: "nginx -t -q && systemctl reload nginx",
	},
	"apache": {
		desc:   "Apache httpd: certificate + key (+ optional chain file), configtest then reload",
		files:  []fileSpec{{"CERT", certs.FullChain, true, false}, {"KEY", certs.Key, true, false}, {"CHAIN", certs.Chain, false, false}},
		health: []string{"systemctl is-active --quiet apache2 || systemctl is-active --quiet httpd", "apachectl configtest"},
		reload: "apachectl configtest && (systemctl reload apache2 2>/dev/null || systemctl reload httpd)",
	},
	"haproxy": {
		desc:   "HAProxy: one combined PEM (chain + key), config check then reload",
		files:  []fileSpec{{"PEM", certs.PEM, true, false}},
		health: []string{"systemctl is-active --quiet haproxy", "haproxy -c -q -f /etc/haproxy/haproxy.cfg"},
		reload: "haproxy -c -q -f /etc/haproxy/haproxy.cfg && systemctl reload haproxy",
	},
	"minio": {
		desc:   "MinIO with its own TLS (--certs-dir): public.crt + private.key, restart",
		files:  []fileSpec{{"CERT", certs.FullChain, true, false}, {"KEY", certs.Key, true, false}},
		health: []string{"systemctl is-active --quiet $SERVICE"},
		reload: "systemctl restart $SERVICE && sleep 3 && systemctl is-active --quiet $SERVICE",
		vars:   []varSpec{{"SERVICE", "minio", false}},
	},
	"postgres": {
		desc:   "PostgreSQL server TLS: certificate + key, reload (key must stay readable by postgres)",
		files:  []fileSpec{{"CERT", certs.FullChain, true, false}, {"KEY", certs.Key, true, false}},
		health: []string{"systemctl is-active --quiet $SERVICE"},
		reload: "systemctl reload $SERVICE",
		vars:   []varSpec{{"SERVICE", "postgresql", false}},
	},
	"k8s": {
		desc: "Kubernetes: updates TLS secrets in every namespace that has them, restarts the workloads that mount them",
		files: []fileSpec{
			{"CERT", certs.FullChain, true, true},
			{"KEY", certs.Key, true, true},
		},
		health:   []string{k8sHealth},
		reload:   k8sReload,
		rollback: k8sRollback,
		state:    k8sState,
		keyMode:  "0600", // only root's kubectl reads these copies
		vars: []varSpec{
			{"TLS_SECRETS", "", true},
			{"OPAQUE_SECRETS", "", false},
			{"RESTART", "", false},
			{"KUBECTL", "kubectl", false},
		},
	},
	"files": {
		desc:  "Only copy files, no service to reload (backup copies, services managed elsewhere)",
		files: []fileSpec{{"CERT", certs.FullChain, false, false}, {"KEY", certs.Key, false, false}, {"CHAIN", certs.Chain, false, false}, {"PEM", certs.PEM, false, false}},
	},
}

// TypeNames lists the built-in target types.
func TypeNames() []string {
	names := make([]string, 0, len(types))
	for n := range types {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// TypeHelp describes every target type and its settings.
func TypeHelp() string {
	var b strings.Builder
	for _, n := range TypeNames() {
		t := types[n]
		fmt.Fprintf(&b, "%-9s %s\n", n, t.desc)
		for _, f := range t.files {
			req := "optional"
			if f.required {
				req = "required"
			}
			fmt.Fprintf(&b, "            <SERVER>_<TARGET>_%-15s %s path, gets %s\n", f.setting, req, f.kind)
		}
		for _, v := range t.vars {
			def := v.def
			if v.required {
				def = "required"
			} else if def == "" {
				def = "optional"
			}
			fmt.Fprintf(&b, "            <SERVER>_<TARGET>_%-15s %s\n", v.setting, def)
		}
	}
	return b.String()
}

// buildTarget reads one target. TARGETS entries are either a type name
// (NGINX) or an id whose type is set with <SERVER>_<ID>_TYPE. Every target
// setting falls back to DEFAULT_<ID>_<SETTING>, so servers with the same
// layout share it.
func buildTarget(f fields, id string) (Target, []string) {
	var errs []string
	fail := func(format string, args ...any) { errs = append(errs, fmt.Sprintf(format, args...)) }
	tf := targetFields{fields: f, id: id}
	t := Target{ID: id, Env: map[string]string{}}

	t.Type = strings.ToLower(tf.get("TYPE"))
	if t.Type == "" {
		t.Type = strings.ToLower(id)
	}
	tt, ok := types[t.Type]
	if !ok {
		fail("target %s: unknown type %q — set %s to one of: %s", id, t.Type, tf.name("TYPE"), strings.Join(TypeNames(), ", "))
		return t, errs
	}

	keyMode := tf.get("KEY_MODE")
	if keyMode == "" {
		keyMode = tt.keyMode
	}
	if m, err := strconv.ParseUint(keyMode, 8, 32); keyMode != "" && (err != nil || m&0o077 == 0o077 || m > 0o777) {
		fail("%s=%q must be an octal mode like 0600 or 0640", tf.name("KEY_MODE"), keyMode)
	} else if keyMode != "" && m&0o007 != 0 {
		fail("%s=%q would make the private key readable by every user", tf.name("KEY_MODE"), keyMode)
	}

	for _, fs := range tt.files {
		dest := tf.get(fs.setting)
		if dest == "" {
			if fs.required {
				fail("%s is required (path of the %s file on the server)", tf.name(fs.setting), strings.ToLower(fs.setting))
			}
			continue
		}
		kind := fs.kind
		if v := strings.ToLower(tf.get(fs.setting + "_CONTENT")); v != "" {
			if !slices.Contains([]certs.Kind{certs.FullChain, certs.Leaf, certs.Chain, certs.Key, certs.PEM}, certs.Kind(v)) {
				fail("%s=%q must be fullchain, leaf, chain, key or pem", tf.name(fs.setting+"_CONTENT"), v)
			}
			kind = certs.Kind(v)
		}
		t.Env[fs.setting] = dest
		t.Files = append(t.Files, File{
			Target:  id,
			Dest:    dest,
			Kind:    kind,
			Create:  tf.bool("CREATE", fs.create, fail),
			Owner:   tf.get("OWNER"),
			KeyMode: keyMode,
		})
	}
	if t.Type == "files" && len(t.Files) == 0 {
		fail("target %s (files): set at least one of %s, _KEY, _CHAIN, _PEM", id, tf.name("CERT"))
	}

	for _, v := range tt.vars {
		val := tf.get(v.setting)
		if val == "" {
			val = v.def
		}
		if val == "" && v.required {
			fail("%s is required", tf.name(v.setting))
		}
		t.Env[v.setting] = val
	}

	t.Health = tt.health
	if v, set := tf.override("HEALTH"); set {
		t.Health = nil
		if v != "" {
			t.Health = []string{v}
		}
	}
	t.Reload = tt.reload
	if v, set := tf.override("RELOAD"); set {
		t.Reload = v
	}
	t.Rollback = tt.rollback
	if v, set := tf.override("ROLLBACK"); set {
		t.Rollback = v
	}
	t.State = tt.state
	return t, errs
}

// targetFields reads <SERVER>_<TARGET>_<NAME>, then DEFAULT_<TARGET>_<NAME>.
type targetFields struct {
	fields
	id string
}

func (tf targetFields) get(name string) string { return tf.fields.get(tf.id + "_" + name) }

// name is the variable to mention in error messages.
func (tf targetFields) name(setting string) string {
	return tf.prefix + "_" + tf.id + "_" + setting
}

func (tf targetFields) bool(name string, def bool, fail func(string, ...any)) bool {
	return tf.fields.bool(tf.id+"_"+name, def, fail)
}

// override reports whether name is set; "none" or "-" mean "set to nothing"
// and disable the built-in default.
func (tf targetFields) override(name string) (string, bool) {
	v := tf.get(name)
	switch strings.ToLower(v) {
	case "":
		return "", false
	case "none", "-":
		return "", true
	}
	return v, true
}
