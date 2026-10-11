package certs

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type pair struct {
	cert *x509.Certificate
	key  *ecdsa.PrivateKey
}

func newCA(t *testing.T) pair {
	return issue(t, pair{}, "Test CA", true, time.Now().Add(365*24*time.Hour))
}

// issue creates a certificate signed by parent (self-signed if parent is empty).
func issue(t *testing.T, parent pair, cn string, ca bool, notAfter time.Time, dns ...string) pair {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber:          big.NewInt(time.Now().UnixNano()),
		Subject:               pkix.Name{CommonName: cn},
		DNSNames:              dns,
		NotBefore:             time.Now().Add(-time.Hour),
		NotAfter:              notAfter,
		IsCA:                  ca,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
	}
	signer, signerKey := tmpl, key
	if parent.cert != nil {
		signer, signerKey = parent.cert, parent.key
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, signer, &key.PublicKey, signerKey)
	if err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	return pair{c, key}
}

func pemCert(cs ...*x509.Certificate) string {
	var b bytes.Buffer
	for _, c := range cs {
		_ = pem.Encode(&b, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
	}
	return b.String()
}

func pemKey(k *ecdsa.PrivateKey) string {
	der, _ := x509.MarshalPKCS8PrivateKey(k)
	return string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}))
}

func write(t *testing.T, name, content string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadRendersAllKinds(t *testing.T) {
	ca := newCA(t)
	leaf := issue(t, ca, "*.example.com", false, time.Now().Add(90*24*time.Hour), "*.example.com", "example.com")
	// CRLF + blank lines, like files edited on Windows or pasted from email
	key := strings.ReplaceAll(pemKey(leaf.key), "\n", "\r\n") + "\r\n\r\n"

	b, err := Load(write(t, "full.crt", pemCert(leaf.cert, ca.cert)), write(t, "k.key", key))
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	for kind, want := range map[Kind]int{FullChain: 2, Leaf: 1, Chain: 1, PEM: 2} {
		if got := strings.Count(string(b.Data(kind)), "BEGIN CERTIFICATE"); got != want {
			t.Errorf("%s: %d certificates, want %d", kind, got, want)
		}
	}
	k := string(b.Data(Key))
	if strings.Contains(k, "\r") || !strings.HasSuffix(k, "-----\n") || strings.Contains(k, "\n\n") {
		t.Errorf("key not normalized: %q", k)
	}
	if !strings.HasSuffix(string(b.Data(PEM)), "PRIVATE KEY-----\n") {
		t.Error("pem must end with the key")
	}
	if err := b.CheckHosts([]string{"apps.example.com", "example.com"}); err != nil {
		t.Errorf("CheckHosts: %v", err)
	}
	if err := b.CheckHosts([]string{"a.b.example.com", "other.com"}); err == nil {
		t.Error("CheckHosts accepted hosts outside the SAN")
	}
}

func TestLoadKeyInCertFile(t *testing.T) {
	leaf := issue(t, pair{}, "x", false, time.Now().Add(90*24*time.Hour), "x")
	if _, err := Load(write(t, "combined.pem", pemCert(leaf.cert)+pemKey(leaf.key)), ""); err != nil {
		t.Fatal(err)
	}
}

func TestLoadRejects(t *testing.T) {
	ca := newCA(t)
	good := issue(t, ca, "a", false, time.Now().Add(90*24*time.Hour), "a.example.com")
	expired := issue(t, ca, "old", false, time.Now().Add(-time.Hour), "a.example.com")
	other := issue(t, ca, "b", false, time.Now().Add(90*24*time.Hour), "b.example.com")

	cases := []struct {
		name, cert, key, want string
	}{
		{"expired", pemCert(expired.cert, ca.cert), pemKey(expired.key), "EXPIRED"},
		{"key from another cert", pemCert(good.cert, ca.cert), pemKey(other.key), "does not match"},
		{"wrong order", pemCert(ca.cert, good.cert), pemKey(good.key), "must come first"},
		{"no key", pemCert(good.cert), "", "no private key"},
		{"not PEM", "hello", pemKey(good.key), "not PEM"},
		{"encrypted key", pemCert(good.cert), "-----BEGIN ENCRYPTED PRIVATE KEY-----\nAAAA\n-----END ENCRYPTED PRIVATE KEY-----\n", "encrypted"},
		{"certificate in key file", pemCert(good.cert), pemKey(good.key) + pemCert(good.cert), "key file also contains certificates"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			keyFile := ""
			if tc.key != "" {
				keyFile = write(t, "k.key", tc.key)
			}
			_, err := Load(write(t, "c.crt", tc.cert), keyFile)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want error containing %q, got %v", tc.want, err)
			}
		})
	}
}

func TestWarnings(t *testing.T) {
	ca := newCA(t)
	soon := issue(t, ca, "s", false, time.Now().Add(5*24*time.Hour), "s")
	b, err := Load(write(t, "c.crt", pemCert(soon.cert)), write(t, "k.key", pemKey(soon.key)))
	if err != nil {
		t.Fatal(err)
	}
	all := strings.Join(b.Warnings, "|")
	for _, want := range []string{"expires soon", "no intermediate"} {
		if !strings.Contains(all, want) {
			t.Errorf("missing warning %q in %q", want, all)
		}
	}
}
