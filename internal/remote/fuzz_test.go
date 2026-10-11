package remote

import (
	"os/exec"
	"strings"
	"testing"
)

// FuzzShq checks that Shq really quotes: a POSIX shell given the quoted
// string must hand back the original bytes, whatever they contain (quotes,
// $(...), backticks, newlines, invalid UTF-8). Every value interpolated into
// commands sent to servers goes through Shq.
func FuzzShq(f *testing.F) {
	for _, s := range []string{
		"", "plain", "it's", "'", "''", `'\''`, `"$HOME"`, "$(id)", "`id`",
		"a b\tc\nd", "-n", "%s %d", `\`, "\xff\xfe", "/etc/ssl/web.key; rm -rf /",
	} {
		f.Add(s)
	}
	sh, err := exec.LookPath("sh")
	if err != nil {
		f.Skip("no POSIX sh")
	}
	f.Fuzz(func(t *testing.T, s string) {
		if strings.Contains(s, "\x00") || len(s) > 1<<16 {
			t.Skip("NUL cannot be passed in an argument; very long strings hit ARG_MAX")
		}
		out, err := exec.Command(sh, "-c", "printf %s "+Shq(s)).Output()
		if err != nil {
			t.Fatalf("sh rejected Shq(%q) = %s: %v", s, Shq(s), err)
		}
		if string(out) != s {
			t.Fatalf("Shq(%q) = %s came back as %q", s, Shq(s), out)
		}
	})
}
