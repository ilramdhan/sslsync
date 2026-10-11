package remote

import (
	"crypto/ed25519"
	"crypto/rand"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// The server offers RSA first but known_hosts only has its ED25519 key:
// the client must ask for ED25519, or the handshake fails as "key changed".
func TestKnownAlgorithmsPrefersRecordedKeyType(t *testing.T) {
	pub, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	sshPub, err := ssh.NewPublicKey(pub)
	if err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(t.TempDir(), "known_hosts")
	line := knownhosts.Line([]string{"host.example.com"}, sshPub) + "\n" +
		knownhosts.Line([]string{"[other.example.com]:2222"}, sshPub) + "\n"
	if err := os.WriteFile(file, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	cb, err := knownhosts.New(file)
	if err != nil {
		t.Fatal(err)
	}

	if got := knownAlgorithms(cb, "host.example.com", 22); !slices.Equal(got, []string{ssh.KeyAlgoED25519}) {
		t.Errorf("host:22 = %v, want [ssh-ed25519]", got)
	}
	if got := knownAlgorithms(cb, "other.example.com", 2222); !slices.Equal(got, []string{ssh.KeyAlgoED25519}) {
		t.Errorf("other:2222 = %v, want [ssh-ed25519]", got)
	}
	if got := knownAlgorithms(cb, "unknown.example.com", 22); got != nil {
		t.Errorf("unknown host = %v, want nil (let SSH negotiate; the callback then reports it unknown)", got)
	}
}
