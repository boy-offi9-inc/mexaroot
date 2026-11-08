package native

import (
	"context"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
)

func TestStartSession(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires a unix shell")
	}
	session, err := New().StartSession(context.Background(), runtimes.SessionRequest{
		Argv: []string{"sh", "-c", "printf hi"},
		Cols: 80,
		Rows: 24,
	})
	if err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	defer session.Close()

	timer := time.AfterFunc(10*time.Second, func() { session.Close() })
	defer timer.Stop()

	var out strings.Builder
	buf := make([]byte, 256)
	for {
		n, err := session.Read(buf)
		out.Write(buf[:n])
		if err != nil {
			break
		}
	}
	if !strings.Contains(out.String(), "hi") {
		t.Fatalf("unexpected output %q", out.String())
	}
}
