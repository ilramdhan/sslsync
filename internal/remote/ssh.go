// Package remote runs commands on a server over SSH, with password or key
// auth, optional sudo, timeouts and errors a human can act on.
package remote

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"github.com/ilramdhan/sslsync/internal/inventory"
)

const (
	DialTimeout    = 15 * time.Second
	DefaultTimeout = 60 * time.Second
)

type Client struct {
	ssh *ssh.Client
	srv *inventory.Server
}

// Dial connects, retrying network errors up to attempts times.
// Authentication and host-key errors are not retried.
func Dial(srv *inventory.Server, knownHostsFile string, attempts int, logf func(string, ...any)) (*Client, error) {
	cc, err := clientConfig(srv, knownHostsFile)
	if err != nil {
		return nil, err
	}
	addr := net.JoinHostPort(srv.Host, strconv.Itoa(srv.Port))
	var lastErr error
	for attempt := 1; attempt <= max(1, attempts); attempt++ {
		c, err := ssh.Dial("tcp", addr, cc)
		if err == nil {
			return &Client{ssh: c, srv: srv}, nil
		}
		msg, retryable := ExplainConnErr(err, srv)
		lastErr = fmt.Errorf("ssh %s: %s", addr, msg)
		if !retryable || attempt >= attempts {
			break
		}
		wait := time.Duration(attempt) * 3 * time.Second
		logf("… attempt %d/%d failed (%s), retrying in %s", attempt, attempts, msg, wait)
		time.Sleep(wait)
	}
	return nil, lastErr
}

func clientConfig(s *inventory.Server, knownHostsFile string) (*ssh.ClientConfig, error) {
	var auths []ssh.AuthMethod
	if s.SSHKey != "" {
		keyData, err := os.ReadFile(s.SSHKey)
		if err != nil {
			return nil, fmt.Errorf("read SSH key: %w", err)
		}
		var signer ssh.Signer
		if s.SSHKeyPassphrase != "" {
			signer, err = ssh.ParsePrivateKeyWithPassphrase(keyData, []byte(s.SSHKeyPassphrase))
		} else {
			signer, err = ssh.ParsePrivateKey(keyData)
		}
		if err != nil {
			return nil, fmt.Errorf("SSH key %s: %w", s.SSHKey, err)
		}
		auths = append(auths, ssh.PublicKeys(signer))
	}
	if s.Password != "" {
		pw := s.Password
		auths = append(auths,
			ssh.Password(pw),
			ssh.KeyboardInteractive(func(_, _ string, qs []string, _ []bool) ([]string, error) {
				ans := make([]string, len(qs))
				for i := range ans {
					ans[i] = pw
				}
				return ans, nil
			}),
		)
	}

	cc := &ssh.ClientConfig{User: s.User, Auth: auths, Timeout: DialTimeout}
	if s.InsecureHostKey {
		cc.HostKeyCallback = ssh.InsecureIgnoreHostKey() //nolint:gosec // only when <SERVER>_INSECURE_HOST_KEY=true
		return cc, nil
	}
	cb, err := knownhosts.New(knownHostsFile)
	if err != nil {
		return nil, fmt.Errorf("known_hosts (%s): %w — run `sslsync trust` first", knownHostsFile, err)
	}
	cc.HostKeyCallback = cb
	// Ask the server for the key type we already trust. Otherwise it may
	// offer e.g. RSA while known_hosts only has its ED25519 key, which
	// knownhosts reports as a changed key.
	cc.HostKeyAlgorithms = knownAlgorithms(cb, s.Host, s.Port)
	return cc, nil
}

// knownAlgorithms returns the host key algorithms known_hosts has for
// host:port, so the handshake negotiates a key that can be verified.
// It probes the callback with a dummy key: the resulting KeyError lists the
// keys on record without contacting the server.
func knownAlgorithms(cb ssh.HostKeyCallback, host string, port int) []string {
	addr := net.JoinHostPort(host, strconv.Itoa(port))
	tcp := &net.TCPAddr{IP: net.IPv4zero, Port: port}
	err := cb(addr, tcp, probeKey{})
	var keyErr *knownhosts.KeyError
	if !errors.As(err, &keyErr) {
		return nil
	}
	var algos []string
	for _, k := range keyErr.Want {
		t := k.Key.Type()
		switch t {
		case ssh.KeyAlgoRSA:
			// prefer SHA-2 signatures; servers have disabled ssh-rsa (SHA-1)
			algos = append(algos, ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256, t)
		default:
			algos = append(algos, t)
		}
	}
	return slices.Compact(algos)
}

// probeKey is a public key that matches nothing in known_hosts.
type probeKey struct{}

func (probeKey) Type() string                        { return "sslsync-probe" }
func (probeKey) Marshal() []byte                     { return []byte("sslsync-probe") }
func (probeKey) Verify([]byte, *ssh.Signature) error { return errors.New("probe key") }

// Close closes the connection.
func (c *Client) Close() error { return c.ssh.Close() }

type RunOpts struct {
	Sudo    bool
	Stdin   []byte
	Timeout time.Duration     // default DefaultTimeout
	Env     map[string]string // exported in the remote shell before cmd
}

// Run executes cmd with `sh -c`. With Sudo the password is fed to `sudo -S`
// on stdin, so it never appears in a process list; without a password
// `sudo -n` is used.
func (c *Client) Run(cmd string, o RunOpts) (string, error) {
	sess, err := c.ssh.NewSession()
	if err != nil {
		return "", err
	}
	defer func() { _ = sess.Close() }() // also closed on timeout below

	full := "sh -c " + Shq(exports(o.Env)+cmd)
	var in bytes.Buffer
	if o.Sudo {
		if c.srv.SudoPassword != "" {
			full = "sudo -S -p '' " + full
			in.WriteString(c.srv.SudoPassword + "\n")
		} else {
			full = "sudo -n " + full
		}
	}
	in.Write(o.Stdin)
	sess.Stdin = &in

	var stdout, stderr bytes.Buffer
	sess.Stdout = &stdout
	sess.Stderr = &stderr

	timeout := o.Timeout
	if timeout == 0 {
		timeout = DefaultTimeout
	}
	done := make(chan error, 1)
	go func() { done <- sess.Run(full) }()
	select {
	case err = <-done:
	case <-time.After(timeout):
		return stdout.String(), fmt.Errorf("timed out after %s", timeout)
	}
	if err != nil {
		msg := explainRemoteErr(stderr.String())
		if msg == "" {
			msg = lastLines(stdout.String(), 3)
		}
		if msg == "" {
			return stdout.String(), err
		}
		return stdout.String(), errors.New(msg)
	}
	return stdout.String(), nil
}

func exports(env map[string]string) string {
	if len(env) == 0 {
		return ""
	}
	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	for _, k := range keys {
		fmt.Fprintf(&b, "export %s=%s\n", k, Shq(env[k]))
	}
	return b.String()
}

// Shq single-quotes a string for POSIX sh.
func Shq(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

func lastLines(s string, n int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.TrimSpace(strings.Join(lines, "; "))
}

// ExplainConnErr turns low-level SSH/network errors into a short reason.
// retryable reports whether trying again might help.
func ExplainConnErr(err error, s *inventory.Server) (msg string, retryable bool) {
	var keyErr *knownhosts.KeyError
	var dnsErr *net.DNSError
	var netErr net.Error
	e := err.Error()

	switch {
	case errors.As(err, &keyErr) && len(keyErr.Want) > 0:
		return fmt.Sprintf("HOST KEY CHANGED for %s — server reinstalled or possible MITM. Verify, then `ssh-keygen -R %s`", s.Host, s.Host), false
	case errors.As(err, &keyErr):
		return fmt.Sprintf("unknown host key — run `sslsync trust --only %s`", s.Name), false
	case strings.Contains(e, "unable to authenticate"), strings.Contains(e, "unexpected message type 51"):
		return fmt.Sprintf("authentication failed — check %s_USER / %s_PASSWORD / %s_SSH_KEY", s.Key, s.Key, s.Key), false
	case errors.As(err, &dnsErr):
		return fmt.Sprintf("hostname %q not found (DNS) — check %s_HOST", s.Host, s.Key), false
	case strings.Contains(e, "connection refused"):
		return fmt.Sprintf("connection refused — check %s_PORT or that sshd runs", s.Key), true
	case strings.Contains(e, "no route to host"), strings.Contains(e, "network is unreachable"):
		return "host unreachable — check IP / VPN / network", true
	case errors.As(err, &netErr) && netErr.Timeout():
		return "connection timed out — wrong IP, firewall, VPN, or server down", true
	case strings.Contains(e, "connection reset"), strings.Contains(e, "EOF"):
		return "connection dropped by server — " + e, true
	}
	return e, true
}

// explainRemoteErr cleans up stderr (drops the sudo lecture) and recognises
// sudo password problems.
func explainRemoteErr(stderr string) string {
	switch {
	case strings.Contains(stderr, "incorrect password"), strings.Contains(stderr, "Sorry, try again"):
		return "sudo password incorrect"
	case strings.Contains(stderr, "a password is required"):
		return "sudo needs a password — set <SERVER>_PASSWORD or <SERVER>_SUDO_PASSWORD"
	case strings.Contains(stderr, "is not in the sudoers"), strings.Contains(stderr, "may not run sudo"):
		return "user is not allowed to use sudo"
	}
	var keep []string
	for _, line := range strings.Split(stderr, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.Contains(line, "lecture") ||
			strings.Contains(line, "Administrator") || strings.Contains(line, "will not be visible") {
			continue
		}
		keep = append(keep, line)
	}
	if len(keep) > 3 {
		keep = keep[len(keep)-3:]
	}
	return strings.Join(keep, "; ")
}
