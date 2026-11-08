package proroot

import (
	"context"
	"path/filepath"
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

func TestStartSessionUsesDefaultShell(t *testing.T) {
	requireUnix(t)
	root := t.TempDir()
	rt := newRuntime(t, root)

	session, err := rt.StartSession(context.Background(), runtimes.SessionRequest{
		Env:  map[string]string{"TERM": "xterm"},
		Cols: 80,
		Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer session.Close()

	got := strings.Fields(readSession(t, session))
	want := launchPrefix(filepath.Join(root, "alpine"), "/root")
	want = append(want,
		"HOME=/root", "LANG=C.UTF-8", baselinePath, "TERM=xterm", "TMPDIR=/tmp",
		"/bin/sh",
	)
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}
