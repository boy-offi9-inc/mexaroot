package hostexec

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
)

func TestRunCapturesOutput(t *testing.T) {
	res, err := Run(context.Background(), Command{
		Name: "sh",
		Args: []string{"-c", "printf out; printf err >&2; exit 4"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if string(res.Stdout) != "out" || string(res.Stderr) != "err" || res.ExitCode != 4 {
		t.Fatalf("unexpected result: %+v", res)
	}
}

func TestRunEmptyName(t *testing.T) {
	_, err := Run(context.Background(), Command{})
	if !errors.Is(err, runtimes.ErrEmptyCommand) {
		t.Fatalf("expected ErrEmptyCommand, got %v", err)
	}
}

func TestRunMissingBinary(t *testing.T) {
	_, err := Run(context.Background(), Command{Name: "mexaroot-no-such-binary"})
	if err == nil || errors.Is(err, runtimes.ErrTimeout) {
		t.Fatalf("expected a start failure, got %v", err)
	}
}

func TestRunTimeout(t *testing.T) {
	_, err := Run(context.Background(), Command{
		Name:    "sh",
		Args:    []string{"-c", "exec sleep 5"},
		Timeout: 100 * time.Millisecond,
	})
	if !errors.Is(err, runtimes.ErrTimeout) {
		t.Fatalf("expected ErrTimeout, got %v", err)
	}
}

func TestMergeEnvAppendsSortedOverrides(t *testing.T) {
	env := MergeEnv(map[string]string{"B": "2", "A": "1"})
	if len(env) < 2 || env[len(env)-2] != "A=1" || env[len(env)-1] != "B=2" {
		t.Fatalf("unexpected tail of environment: %v", env)
	}
}
