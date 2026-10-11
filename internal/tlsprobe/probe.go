// Package tlsprobe connects to a TLS port and returns the certificate it
// serves. It does not trust or validate the certificate — callers compare it
// byte-for-byte with the expected one.
package tlsprobe

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net"
	"time"
)

const Timeout = 8 * time.Second

// ErrOld means the port answered but still serves another certificate.
var ErrOld = errors.New("still serves the old certificate")

// Fetch returns the leaf certificate served at addr (host:port), with the
// host part as SNI so name-based virtual hosts answer correctly.
func Fetch(addr string) (*x509.Certificate, error) {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, err
	}
	conn, err := tls.DialWithDialer(&net.Dialer{Timeout: Timeout}, "tcp", addr, &tls.Config{
		ServerName:         host,
		InsecureSkipVerify: true, //nolint:gosec // the certificate is compared exactly by the caller
	})
	if err != nil {
		return nil, err
	}
	defer func() { _ = conn.Close() }()
	certs := conn.ConnectionState().PeerCertificates
	if len(certs) == 0 {
		return nil, errors.New("no certificate presented")
	}
	return certs[0], nil
}

// WaitFor polls addr until it serves want — a service may need a moment
// after a reload. It returns the last certificate seen.
func WaitFor(addr string, want *x509.Certificate, attempts int, interval time.Duration) (*x509.Certificate, error) {
	var last *x509.Certificate
	var lastErr error
	for i := range attempts {
		if i > 0 {
			time.Sleep(interval)
		}
		c, err := Fetch(addr)
		if err != nil {
			lastErr = err
			continue
		}
		last, lastErr = c, nil
		if c.Equal(want) {
			return c, nil
		}
	}
	if lastErr != nil {
		return last, lastErr
	}
	return last, ErrOld
}
