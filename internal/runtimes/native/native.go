package native

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
)

const waitDelay = 2 * time.Second

type Runtime struct {
	id   string
	name string
}

func New() *Runtime {
	return &Runtime{id: "termux", name: "Termux"}
}

func (r *Runtime) Info() runtimes.Info {
	return runtimes.Info{
		ID:    r.id,
		Kind:  runtimes.KindTermux,
		Name:  r.name,
		State: runtimes.StateAvailable,
	}
}

func (r *Runtime) Exec(ctx context.Context, req runtimes.ExecRequest) (runtimes.ExecResult, error) {
	if len(req.Argv) == 0 {
		return runtimes.ExecResult{}, runtimes.ErrEmptyCommand
	}
	if req.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}

	cmd := exec.CommandContext(ctx, req.Argv[0], req.Argv[1:]...)
	cmd.Dir = req.Cwd
	cmd.Env = mergeEnv(req.Env)
	cmd.WaitDelay = waitDelay

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	result := runtimes.ExecResult{Stdout: stdout.Bytes(), Stderr: stderr.Bytes()}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, runtimes.ErrTimeout
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("run %s: %w", req.Argv[0], err)
	}
	return result, nil
}

func mergeEnv(extra map[string]string) []string {
	env := os.Environ()
	keys := make([]string, 0, len(extra))
	for key := range extra {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		env = append(env, key+"="+extra[key])
	}
	return env
}
