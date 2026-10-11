package app

import (
	"bytes"
	"os"
	"strings"
	"testing"
)

func TestEmbeddedTemplateMatchesRepo(t *testing.T) {
	repo, err := os.ReadFile("../../.env.example")
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(repo, envExample) {
		t.Fatal("internal/app/env.example is out of date — run: make sync-template")
	}
}

func TestInit(t *testing.T) {
	t.Chdir(t.TempDir())
	var out bytes.Buffer
	if code := Main([]string{"init"}, &out, &out); code != 0 {
		t.Fatalf("init exit %d: %s", code, out.String())
	}
	st, err := os.Stat(".env")
	if err != nil || st.Mode().Perm() != 0o600 {
		t.Fatalf(".env: %v mode %v", err, st.Mode())
	}
	if _, err := os.Stat("certs"); err != nil {
		t.Fatal(err)
	}

	// second run must not overwrite an edited .env
	if err := os.WriteFile(".env", []byte("SSLSYNC_SERVERS=MINE\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	out.Reset()
	Main([]string{"init"}, &out, &out)
	if got, _ := os.ReadFile(".env"); string(got) != "SSLSYNC_SERVERS=MINE\n" {
		t.Fatalf(".env was overwritten: %q", got)
	}
	if !strings.Contains(out.String(), "already exists") {
		t.Errorf("expected 'already exists' in %q", out.String())
	}
}

func TestVersion(t *testing.T) {
	var out bytes.Buffer
	if Main([]string{"version"}, &out, &out) != 0 || !strings.HasPrefix(out.String(), "sslsync ") {
		t.Fatalf("version: %q", out.String())
	}
}
