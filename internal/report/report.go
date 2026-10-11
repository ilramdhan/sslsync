// Package report records what a run did: on the terminal, in one log file
// per run and per server, and in a machine-readable summary.json.
//
//	logs/<stamp>_<command>/run.log        everything, as shown on screen
//	logs/<stamp>_<command>/<server>.log   one server, with timestamps
//	logs/<stamp>_<command>/summary.json   status per server and per phase
package report

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const stampFormat = "20060102-150405"

type Run struct {
	Dir     string
	Command string
	Stamp   string // unique per run; also names the server-side backups

	mu      sync.Mutex
	out     io.Writer
	files   []*os.File
	started time.Time
}

// New starts a run. With logRoot == "" nothing is written to disk.
func New(logRoot, command string) (*Run, error) {
	now := time.Now()
	r := &Run{Command: command, Stamp: now.Format(stampFormat), out: os.Stdout, started: now}
	if logRoot == "" {
		return r, nil
	}
	if err := os.MkdirAll(logRoot, 0o700); err != nil {
		return nil, err
	}
	// Mkdir (not MkdirAll) fails when the dir exists, so two runs started in
	// the same second never share a log dir or a backup stamp.
	for i := 0; ; i++ {
		if i > 0 {
			r.Stamp = fmt.Sprintf("%s-%d", now.Format(stampFormat), i)
		}
		r.Dir = filepath.Join(logRoot, r.Stamp+"_"+command)
		err := os.Mkdir(r.Dir, 0o700)
		if err == nil {
			break
		}
		if !errors.Is(err, os.ErrExist) || i >= 50 {
			return nil, err
		}
	}
	f, err := r.create("run.log")
	if err != nil {
		return nil, err
	}
	r.out = io.MultiWriter(os.Stdout, f)
	return r, nil
}

func (r *Run) create(name string) (*os.File, error) {
	f, err := os.OpenFile(filepath.Join(r.Dir, name), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err == nil {
		r.files = append(r.files, f)
	}
	return f, err
}

// Printf writes to the screen and run.log. Output errors are ignored on
// purpose: a full disk must not abort a deploy half-way.
func (r *Run) Printf(format string, args ...any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	_, _ = fmt.Fprintf(r.out, format, args...)
}

// Server returns a logger that prefixes lines with the server name and also
// writes them, timestamped, to <server>.log.
func (r *Run) Server(name string) *Logger {
	l := &Logger{run: r, name: name}
	if r.Dir != "" {
		l.file, _ = r.create(name + ".log") // per-server file is best-effort; run.log has everything
	}
	return l
}

type Logger struct {
	run  *Run
	name string
	file *os.File
}

func (l *Logger) Logf(format string, args ...any) {
	msg := fmt.Sprintf(format, args...)
	l.run.Printf("[%s] %s\n", l.name, msg)
	if l.file != nil {
		_, _ = fmt.Fprintf(l.file, "%s %s\n", time.Now().Format(time.TimeOnly), msg)
	}
}

// Phase is one step of the work on a server.
type Phase struct {
	Name     string `json:"name"`
	OK       bool   `json:"ok"`
	Detail   string `json:"detail,omitempty"`
	Duration string `json:"duration"`
}

// ServerResult is what summary.json records for one server.
type ServerResult struct {
	Server   string   `json:"server"`
	Group    string   `json:"group,omitempty"`
	Host     string   `json:"host"`
	Status   string   `json:"status"`
	OK       bool     `json:"ok"`
	Error    string   `json:"error,omitempty"`
	Changes  []string `json:"changes,omitempty"`
	Phases   []Phase  `json:"phases"`
	Started  string   `json:"started"`
	Duration string   `json:"duration"`
}

type summary struct {
	Command  string         `json:"command"`
	Started  string         `json:"started"`
	Duration string         `json:"duration"`
	Cert     map[string]any `json:"certificate,omitempty"`
	Servers  []ServerResult `json:"servers"`
}

// Finish writes summary.json and closes all log files.
func (r *Run) Finish(cert map[string]any, results []ServerResult) error {
	var errs []error
	if r.Dir != "" {
		data, err := json.MarshalIndent(summary{
			Command:  r.Command,
			Started:  r.started.Format(time.RFC3339),
			Duration: time.Since(r.started).Round(time.Second).String(),
			Cert:     cert,
			Servers:  results,
		}, "", "  ")
		if err == nil {
			err = os.WriteFile(filepath.Join(r.Dir, "summary.json"), data, 0o600)
		}
		errs = append(errs, err)
	}
	for _, f := range r.files {
		errs = append(errs, f.Close())
	}
	return errors.Join(errs...)
}
