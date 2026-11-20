package native

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
)

func TestInfo(t *testing.T) {
	info := New().Info()
	// On Android/Termux the native runtime is termux; everywhere else it is
	// the plain host runtime.
	if runtime.GOOS == "android" || os.Getenv("TERMUX_VERSION") != "" {
		if info.ID != "termux" || info.Kind != runtimes.KindTermux {
			t.Fatalf("expected termux runtime, got: %+v", info)
		}
		return
	}
	if info.ID != "host" || info.Kind != runtimes.KindHost || info.State != runtimes.StateAvailable {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestIsTermux(t *testing.T) {
	if runtime.GOOS == "android" {
		t.Skip("android build always reports termux")
	}
	os.Unsetenv("TERMUX_VERSION")
	if isTermux() {
		t.Fatal("expected isTermux=false without TERMUX_VERSION")
	}
	t.Setenv("TERMUX_VERSION", "0.118.0")
	if !isTermux() {
		t.Fatal("expected isTermux=true with TERMUX_VERSION set")
	}
}

func TestExecCapturesOutput(t *testing.T) {
	res, err := New().Exec(context.Background(), runtimes.ExecRequest{
		Argv: []string{"sh", "-c", "printf out; printf err >&2"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if string(res.Stdout) != "out" || string(res.Stderr) != "err" || res.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestExecReportsExitCode(t *testing.T) {
	res, err := New().Exec(context.Background(), runtimes.ExecRequest{
		Argv: []string{"sh", "-c", "exit 3"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	if res.ExitCode != 3 {
		t.Fatalf("expected exit code 3, got %d", res.ExitCode)
	}
}

func TestExecAppliesEnvAndCwd(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatalf("EvalSymlinks: %v", err)
	}
	res, err := New().Exec(context.Background(), runtimes.ExecRequest{
		Argv: []string{"sh", "-c", `printf "%s:%s" "$MEXAROOT_TEST" "$(pwd -P)"`},
		Cwd:  dir,
		Env:  map[string]string{"MEXAROOT_TEST": "value"},
	})
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	want := "value:" + dir
	if string(res.Stdout) != want {
		t.Fatalf("got %q, want %q", res.Stdout, want)
	}
}

func TestExecTimeout(t *testing.T) {
	_, err := New().Exec(context.Background(), runtimes.ExecRequest{
		Argv:    []string{"sh", "-c", "exec sleep 5"},
		Timeout: 100 * time.Millisecond,
	})
	if !errors.Is(err, runtimes.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
}

func TestExecEmptyCommand(t *testing.T) {
	_, err := New().Exec(context.Background(), runtimes.ExecRequest{})
	if !errors.Is(err, runtimes.ErrEmptyCommand) {
		t.Fatalf("expected ErrEmptyCommand, got %v", err)
	}
}

func TestExecMissingBinary(t *testing.T) {
	_, err := New().Exec(context.Background(), runtimes.ExecRequest{
		Argv: []string{"mexaroot-no-such-binary"},
	})
	if err == nil || errors.Is(err, runtimes.ErrTimeout) {
		t.Fatalf("expected a start failure, got %v", err)
	}
}
