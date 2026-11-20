package server

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAuditDisabledByDefault(t *testing.T) {
	a, err := newAuditLog("")
	if err != nil {
		t.Fatalf("newAuditLog(\"\"): %v", err)
	}
	defer a.close()
	a.write(AuditEntry{Event: "exec", Argv: []string{"ls"}})
	if enabled, _, _ := a.status(); enabled {
		t.Fatal("empty path should mean disabled")
	}
}

func TestAuditWritesJSONLines(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	a, err := newAuditLog(path)
	if err != nil {
		t.Fatalf("newAuditLog: %v", err)
	}
	code := 0
	a.write(AuditEntry{Event: "connect", RemoteIP: "203.0.113.7"})
	a.write(AuditEntry{Event: "exec", RemoteIP: "203.0.113.7", Runtime: "termux", Argv: []string{"uname", "-a"}, ExitCode: &code})
	if err := a.close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	var first AuditEntry
	if err := json.Unmarshal([]byte(lines[0]), &first); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if first.Event != "connect" || first.RemoteIP != "203.0.113.7" {
		t.Errorf("unexpected entry: %+v", first)
	}
	if _, err := time.Parse(time.RFC3339, first.Time); err != nil {
		t.Errorf("time not RFC 3339: %v", first.Time)
	}
	var second AuditEntry
	if err := json.Unmarshal([]byte(lines[1]), &second); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if second.Runtime != "termux" || len(second.Argv) != 2 || second.ExitCode == nil || *second.ExitCode != 0 {
		t.Errorf("unexpected exec entry: %+v", second)
	}
}

func TestAuditFileIsMode0600(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	a, err := newAuditLog(path)
	if err != nil {
		t.Fatalf("newAuditLog: %v", err)
	}
	a.write(AuditEntry{Event: "connect"})
	if err := a.close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("audit log mode = %o, want 600", perm)
	}
}

func TestAuditAppendsAcrossInstances(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	a1, err := newAuditLog(path)
	if err != nil {
		t.Fatalf("newAuditLog: %v", err)
	}
	a1.write(AuditEntry{Event: "connect", RemoteIP: "1.2.3.4"})
	a1.close()

	a2, err := newAuditLog(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	a2.write(AuditEntry{Event: "connect", RemoteIP: "5.6.7.8"})
	a2.close()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if n := strings.Count(string(data), "\n"); n != 2 {
		t.Errorf("expected 2 appended lines, got %d: %q", n, data)
	}
}

func TestAuditWriteFailureIsCountedNotFatal(t *testing.T) {
	// Point the log at a directory: OpenFile with O_CREATE succeeds? No —
	// creating a file over a directory fails, so test the in-memory counter
	// by closing the file first and writing after.
	dir := t.TempDir()
	path := filepath.Join(dir, "audit.jsonl")
	a, err := newAuditLog(path)
	if err != nil {
		t.Fatalf("newAuditLog: %v", err)
	}
	if err := a.f.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	a.write(AuditEntry{Event: "exec"}) // must not panic
	_, errs, last := a.status()
	if errs == 0 {
		t.Error("a write to a closed file should be counted as an error")
	}
	if last == "" {
		t.Error("lastError should describe the failure")
	}
}

func TestAuditBadPathFailsNew(t *testing.T) {
	if _, err := newAuditLog("/proc/definitely/not/a/real/dir/audit.jsonl"); err == nil {
		t.Error("expected an error for an uncreatable path")
	}
}
