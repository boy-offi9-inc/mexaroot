package server

import (
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// Audit log: an append-only JSON Lines record of connections and executed
// commands, off by default (`Config.AuditPath` empty disables it) and written
// with 0600 so only the daemon's user can read it - the entries name
// runtimes and argv, which is exactly what an eavesdropper would want.
//
// Auditing never blocks or fails a request: a write error is counted and
// logged once, and the last error is kept for the status report.

// AuditEntry is one JSON Lines record. `Time` is RFC 3339 in UTC.
type AuditEntry struct {
	Time     string   `json:"time"`
	Event    string   `json:"event"` // "connect" | "auth_failed" | "exec" | "session_start" | "disconnect"
	RemoteIP string   `json:"remote_ip,omitempty"`
	Runtime  string   `json:"runtime,omitempty"`
	Detail   string   `json:"detail,omitempty"`
	Argv     []string `json:"argv,omitempty"`
	ExitCode *int     `json:"exit_code,omitempty"`
	Error    string   `json:"error,omitempty"`
}

type auditLog struct {
	mu          sync.Mutex
	f           *os.File
	writeErrors int
	lastError   string
}

func newAuditLog(path string) (*auditLog, error) {
	if path == "" {
		return &auditLog{}, nil // disabled
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit log: %w", err)
	}
	return &auditLog{f: f}, nil
}

// write appends one entry. Never returns an error to the caller - auditing
// must not break the request it is recording - but a failed write is counted.
func (a *auditLog) write(entry AuditEntry) {
	if a == nil || a.f == nil {
		return
	}
	if entry.Time == "" {
		entry.Time = time.Now().UTC().Format(time.RFC3339)
	}
	data, err := json.Marshal(entry)
	if err != nil {
		a.noteFailure(err)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if _, err := a.f.Write(append(data, '\n')); err != nil {
		a.writeErrors++
		a.lastError = err.Error()
	}
}

func (a *auditLog) noteFailure(err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.writeErrors++
	a.lastError = err.Error()
}

// status reports whether the log is enabled and how it is doing, for the
// daemon's startup line and `mexaroot doctor`.
func (a *auditLog) status() (enabled bool, writeErrors int, lastError string) {
	if a == nil || a.f == nil {
		return false, 0, ""
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	return true, a.writeErrors, a.lastError
}

func (a *auditLog) close() error {
	if a == nil || a.f == nil {
		return nil
	}
	return a.f.Close()
}
