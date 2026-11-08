package prootdistro

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
)

func readSession(t *testing.T, session runtimes.Session) string {
	t.Helper()
	timer := time.AfterFunc(10*time.Second, func() { session.Close() })
	defer timer.Stop()

	var out strings.Builder
	buf := make([]byte, 1024)
	for {
		n, err := session.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			return out.String()
		}
	}
}

func sessionArgs(t *testing.T, req runtimes.SessionRequest) []string {
	t.Helper()
	requireUnix(t)
	rt, err := New("alpine", Config{Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	session, err := rt.StartSession(context.Background(), req)
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer session.Close()
	return strings.Fields(readSession(t, session))
}

func TestStartSessionLoginShell(t *testing.T) {
	got := sessionArgs(t, runtimes.SessionRequest{Cols: 80, Rows: 24})
	want := []string{"login", "alpine"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStartSessionWithCommand(t *testing.T) {
	got := sessionArgs(t, runtimes.SessionRequest{
		Argv: []string{"top"},
		Cwd:  "/root",
		Env:  map[string]string{"TERM": "xterm"},
	})
	want := []string{"login", "alpine", "--work-dir", "/root", "--", "env", "TERM=xterm", "top"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestStartSessionEnvWithoutCommandUsesShell(t *testing.T) {
	got := sessionArgs(t, runtimes.SessionRequest{Env: map[string]string{"TERM": "xterm"}})
	want := []string{"login", "alpine", "--", "env", "TERM=xterm", "/bin/sh", "-l"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
