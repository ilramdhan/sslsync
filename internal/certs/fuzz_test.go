package certs

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/pem"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// FuzzLoad feeds arbitrary bytes as the certificate and key files. Load must
// never panic, and whatever it accepts must render to the normalized PEM the
// servers receive: canonical blocks, LF only, no blank lines, and SHA values
// that match the bytes actually written. An empty key means "the key is in
// the certificate file".
func FuzzLoad(f *testing.F) {
	ca := newCA(f)
	leaf := issue(f, ca, "a", false, time.Now().Add(90*24*time.Hour), "a.example.com")
	full, key := pemCert(leaf.cert, ca.cert), pemKey(leaf.key)
	crlf := func(s string) string { return strings.ReplaceAll(s, "\n", "\r\n") + "\r\n\r\n" }

	f.Add([]byte(full), []byte(key))
	f.Add([]byte(crlf(full)), []byte(crlf(key)))
	f.Add([]byte(full+key), []byte{})
	f.Add([]byte(pemCert(leaf.cert)), []byte(key))
	f.Add([]byte("hello"), []byte(key))
	f.Add([]byte{}, []byte{})
	f.Add([]byte(full), []byte(key+full))
	f.Add([]byte(key), []byte(key))
	f.Add([]byte("-----BEGIN CERTIFICATE-----\nAAAA\n-----END CERTIFICATE-----\n"), []byte("\x00\xff"))

	f.Fuzz(func(t *testing.T, certData, keyData []byte) {
		dir := t.TempDir()
		certFile := filepath.Join(dir, "c.crt")
		if err := os.WriteFile(certFile, certData, 0o600); err != nil {
			t.Fatal(err)
		}
		keyFile := ""
		if len(keyData) > 0 {
			keyFile = filepath.Join(dir, "k.key")
			if err := os.WriteFile(keyFile, keyData, 0o600); err != nil {
				t.Fatal(err)
			}
		}

		b, err := Load(certFile, keyFile)
		if err != nil {
			return
		}
		if b.Leaf == nil {
			t.Fatal("Load succeeded without a leaf certificate")
		}
		for _, k := range []Kind{FullChain, Leaf, Chain, Key, PEM} {
			d := b.Data(k)
			if bytes.Contains(d, []byte("\r")) || bytes.Contains(d, []byte("\n\n")) || bytes.HasPrefix(d, []byte("\n")) {
				t.Errorf("%s: not normalized: %q", k, d)
			}
			sum := sha256.Sum256(d)
			if got := b.SHA(k); got != hex.EncodeToString(sum[:]) {
				t.Errorf("%s: SHA %s does not match its data", k, got)
			}
		}

		fullChain := b.Data(FullChain)
		if len(fullChain) == 0 {
			t.Fatal("empty full chain")
		}
		blocks := decodeAll(t, fullChain)
		if len(blocks) != 1+len(b.Chain) {
			t.Fatalf("full chain has %d blocks, want %d", len(blocks), 1+len(b.Chain))
		}
		for i, blk := range blocks {
			if blk.Type != "CERTIFICATE" || len(blk.Headers) != 0 {
				t.Fatalf("full chain block %d is %q with headers %v", i, blk.Type, blk.Headers)
			}
		}
		if !bytes.Equal(blocks[0].Bytes, b.Leaf.Raw) {
			t.Fatal("full chain does not start with the leaf")
		}

		k := b.Data(Key)
		if !bytes.HasSuffix(k, []byte("-----\n")) {
			t.Fatalf("key does not end with a PEM footer: %q", k)
		}
		kb := decodeAll(t, k)
		if len(kb) != 1 || !strings.HasSuffix(kb[0].Type, "PRIVATE KEY") || len(kb[0].Headers) != 0 {
			t.Fatalf("key file is not exactly one private key block: %q", k)
		}
		if !bytes.Equal(b.Data(PEM), append(append([]byte{}, fullChain...), k...)) {
			t.Fatal("pem is not full chain + key")
		}
	})
}

// decodeAll decodes every PEM block in data, failing on anything that is
// not PEM and on blocks that do not re-encode to exactly the same bytes.
func decodeAll(t *testing.T, data []byte) []*pem.Block {
	t.Helper()
	var out []*pem.Block
	rest := data
	for len(rest) > 0 {
		blk, r := pem.Decode(rest)
		if blk == nil {
			t.Fatalf("not PEM: %q", rest)
		}
		raw := rest[:len(rest)-len(r)]
		if enc := pem.EncodeToMemory(blk); !bytes.Equal(enc, raw) {
			t.Fatalf("block is not canonical PEM:\n%q\nre-encodes to\n%q", raw, enc)
		}
		out = append(out, blk)
		rest = r
	}
	return out
}
