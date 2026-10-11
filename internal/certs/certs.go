// Package certs loads a certificate + private key pair from local files,
// validates it, and renders it in the formats servers need (full chain, leaf,
// chain, key, combined PEM). A bad pair is rejected before any server is
// touched.
package certs

import (
	"bytes"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/pem"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"
)

// ExpirySoon is when a certificate starts producing a warning.
const ExpirySoon = 14 * 24 * time.Hour

// Kind is a rendering of the pair that a destination file receives.
type Kind string

const (
	FullChain Kind = "fullchain" // leaf + intermediates (nginx, minio, most services)
	Leaf      Kind = "leaf"      // leaf only (apache with a separate chain file)
	Chain     Kind = "chain"     // intermediates only
	Key       Kind = "key"       // private key
	PEM       Kind = "pem"       // full chain + key in one file (haproxy)
)

// Secret reports whether files of this kind contain the private key.
func (k Kind) Secret() bool { return k == Key || k == PEM }

// Bundle is a validated certificate + key pair.
type Bundle struct {
	CertFile string
	KeyFile  string
	Leaf     *x509.Certificate
	Chain    []*x509.Certificate
	Warnings []string

	data map[Kind][]byte
}

// Data returns the normalized file content for k: canonical PEM, LF line
// endings, no blank lines, one trailing newline. MinIO refuses keys with
// trailing blank lines ("The private key contains additional data").
func (b *Bundle) Data(k Kind) []byte { return b.data[k] }

// SHA returns the sha256 of Data(k) as hex.
func (b *Bundle) SHA(k Kind) string {
	sum := sha256.Sum256(b.data[k])
	return hex.EncodeToString(sum[:])
}

// Load reads and validates the pair. keyFile may be empty when certFile also
// contains the private key.
func Load(certFile, keyFile string) (*Bundle, error) {
	certs, certKey, err := readPEM(certFile)
	if err != nil {
		return nil, err
	}
	key := certKey
	if keyFile != "" && keyFile != certFile {
		var keyCerts []*x509.Certificate
		keyCerts, key, err = readPEM(keyFile)
		if err != nil {
			return nil, err
		}
		if len(keyCerts) > 0 {
			return nil, fmt.Errorf("%s: key file also contains certificates — is it the right file?", keyFile)
		}
	}
	if len(certs) == 0 {
		return nil, fmt.Errorf("%s: no certificate found", certFile)
	}
	if key == nil {
		return nil, fmt.Errorf("no private key: set the key file, or put the key in %s", certFile)
	}

	b := &Bundle{CertFile: certFile, KeyFile: keyFile, Leaf: certs[0], Chain: certs[1:]}
	if err := b.checkKey(certs, key); err != nil {
		return nil, err
	}
	if err := b.checkDates(); err != nil {
		return nil, err
	}
	b.checkChain()
	b.render(key)
	return b, nil
}

func readPEM(path string) (certs []*x509.Certificate, key *pem.Block, err error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	rest := bytes.ReplaceAll(raw, []byte("\r"), nil)
	for {
		var block *pem.Block
		block, rest = pem.Decode(rest)
		if block == nil {
			break
		}
		switch {
		case block.Type == "CERTIFICATE":
			c, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				return nil, nil, fmt.Errorf("%s: %w", path, err)
			}
			certs = append(certs, c)
		case strings.HasSuffix(block.Type, "PRIVATE KEY"):
			if key != nil {
				return nil, nil, fmt.Errorf("%s: more than one private key", path)
			}
			if strings.Contains(block.Type, "ENCRYPTED") || block.Headers["Proc-Type"] != "" {
				return nil, nil, fmt.Errorf("%s: private key is encrypted — servers need it unencrypted (openssl pkey -in %s -out new.key)", path, path)
			}
			key = block
		}
	}
	if len(bytes.TrimSpace(rest)) > 0 {
		return nil, nil, fmt.Errorf("%s: contains data that is not PEM (is it DER/.pfx? convert with openssl)", path)
	}
	if len(certs) == 0 && key == nil {
		return nil, nil, fmt.Errorf("%s: no PEM certificate or key found", path)
	}
	return certs, key, nil
}

// checkKey makes sure the key belongs to the first certificate. A key that
// matches a later certificate means the file is in the wrong order, which
// makes servers present the wrong certificate.
func (b *Bundle) checkKey(certs []*x509.Certificate, key *pem.Block) error {
	keyPEM := pem.EncodeToMemory(key)
	for i, c := range certs {
		_, err := tls.X509KeyPair(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: c.Raw}), keyPEM)
		if err != nil {
			continue
		}
		if i > 0 {
			return fmt.Errorf("%s: the certificate for this key is #%d in the file — the server certificate must come first", b.CertFile, i+1)
		}
		return nil
	}
	return errors.New("private key does not match the certificate (wrong key file, or a key from an older CSR)")
}

func (b *Bundle) checkDates() error {
	now := time.Now()
	switch {
	case now.After(b.Leaf.NotAfter):
		return fmt.Errorf("certificate EXPIRED on %s", b.Leaf.NotAfter.Format(time.DateOnly))
	case now.Before(b.Leaf.NotBefore):
		return fmt.Errorf("certificate not valid before %s", b.Leaf.NotBefore.Format(time.DateTime))
	case b.Leaf.NotAfter.Sub(now) < ExpirySoon:
		b.Warnings = append(b.Warnings, fmt.Sprintf("certificate expires soon (%s)", b.Leaf.NotAfter.Format(time.DateOnly)))
	}
	for _, c := range b.Chain {
		if now.After(c.NotAfter) {
			b.Warnings = append(b.Warnings, fmt.Sprintf("intermediate %q expired on %s", c.Subject.CommonName, c.NotAfter.Format(time.DateOnly)))
		}
	}
	return nil
}

func (b *Bundle) checkChain() {
	if len(b.Chain) == 0 {
		if !isSelfSigned(b.Leaf) {
			b.Warnings = append(b.Warnings, "no intermediate certificates — use a full-chain file (cert + ca-bundle), or some clients will reject it")
		}
		return
	}
	pool := x509.NewCertPool()
	for _, c := range b.Chain {
		pool.AddCert(c)
	}
	if _, err := b.Leaf.Verify(x509.VerifyOptions{Intermediates: pool}); err != nil {
		b.Warnings = append(b.Warnings, "chain does not verify against this Mac's trusted roots: "+err.Error())
	}
}

func isSelfSigned(c *x509.Certificate) bool {
	return bytes.Equal(c.RawIssuer, c.RawSubject) && c.CheckSignatureFrom(c) == nil
}

func (b *Bundle) render(key *pem.Block) {
	enc := func(cs ...*x509.Certificate) []byte {
		var buf bytes.Buffer
		for _, c := range cs {
			_ = pem.Encode(&buf, &pem.Block{Type: "CERTIFICATE", Bytes: c.Raw})
		}
		return buf.Bytes()
	}
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: key.Type, Bytes: key.Bytes})
	full := enc(append([]*x509.Certificate{b.Leaf}, b.Chain...)...)
	b.data = map[Kind][]byte{
		FullChain: full,
		Leaf:      enc(b.Leaf),
		Chain:     enc(b.Chain...),
		Key:       keyPEM,
		PEM:       append(append([]byte{}, full...), keyPEM...),
	}
}

// CheckHosts reports the hosts the certificate does not cover.
func (b *Bundle) CheckHosts(hosts []string) error {
	var bad []string
	for _, h := range hosts {
		if b.Leaf.VerifyHostname(h) != nil {
			bad = append(bad, h)
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("certificate (SAN %v) does not cover %s", b.Leaf.DNSNames, strings.Join(bad, ", "))
	}
	return nil
}

// Fingerprint is a short SHA-256 of a certificate, for logs.
func Fingerprint(c *x509.Certificate) string {
	sum := sha256.Sum256(c.Raw)
	return strings.ToUpper(hex.EncodeToString(sum[:]))[:16]
}

// Describe returns a one-line summary of a certificate.
func Describe(c *x509.Certificate) string {
	days := int(time.Until(c.NotAfter).Hours() / 24)
	return fmt.Sprintf("%s %v, issuer %q, valid %s → %s (%d days), sha256 %s",
		c.Subject.CommonName, c.DNSNames, c.Issuer.CommonName,
		c.NotBefore.Format(time.DateOnly), c.NotAfter.Format(time.DateOnly), days, Fingerprint(c))
}
