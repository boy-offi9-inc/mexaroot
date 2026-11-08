//go:build !windows

package hostexec

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
)

func drain(t *testing.T, session runtimes.Session) string {
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

func TestStartPTYRunsCommand(t *testing.T) {
	session, err := StartPTY(Command{Name: "sh", Args: []string{"-c", "printf hello"}}, 80, 24)
	if err != nil {
		t.Fatalf("StartPTY: %v", err)
	}
	defer session.Close()

	if out := drain(t, session); !strings.Contains(out, "hello") {
		t.Fatalf("unexpected output %q", out)
	}
	code, err := session.Wait()
	if err != nil || code != 0 {
		t.Fatalf("Wait = %d, %v", code, err)
	}
}

func TestStartPTYAcceptsInput(t *testing.T) {
	session, err := StartPTY(Command{
		Name: "sh",
		Args: []string{"-c", `read line; printf "got:%s" "$line"`},
	}, 80, 24)
	if err != nil {
		t.Fatalf("StartPTY: %v", err)
	}
	defer session.Close()

	if _, err := session.Write([]byte("abc\n")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if out := drain(t, session); !strings.Contains(out, "got:abc") {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestStartPTYResize(t *testing.T) {
	session, err := StartPTY(Command{
		Name: "sh",
		Args: []string{"-c", "sleep 1; stty size"},
	}, 80, 24)
	if err != nil {
		t.Fatalf("StartPTY: %v", err)
	}
	defer session.Close()

	if err := session.Resize(100, 30); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if out := drain(t, session); !strings.Contains(out, "30 100") {
		t.Fatalf("unexpected output %q", out)
	}
}

func TestStartPTYEmptyName(t *testing.T) {
	if _, err := StartPTY(Command{}, 80, 24); !errors.Is(err, runtimes.ErrEmptyCommand) {
		t.Fatalf("expected ErrEmptyCommand, got %v", err)
	}
}

func TestStartPTYCloseTerminatesProcess(t *testing.T) {
	session, err := StartPTY(Command{Name: "sleep", Args: []string{"30"}}, 80, 24)
	if err != nil {
		t.Fatalf("StartPTY: %v", err)
	}
	session.Close()

	done := make(chan struct{})
	go func() {
		session.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("process was not terminated by Close")
	}
}
