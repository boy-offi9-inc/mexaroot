package hostexec

import (
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

type Command struct {
	Name    string
	Args    []string
	Dir     string
	Env     []string
	Timeout time.Duration
}

func Run(ctx context.Context, c Command) (runtimes.ExecResult, error) {
	if c.Name == "" {
		return runtimes.ExecResult{}, runtimes.ErrEmptyCommand
	}
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}

	// Running the client's chosen binary is the product: argv exec, no
	// shell, and the caller is already authenticated.
	cmd := exec.CommandContext(ctx, c.Name, c.Args...) // codeql[go/command-injection] direct argv exec of the authenticated client's own request, no shell
	cmd.Dir = c.Dir
	cmd.Env = c.Env
	cmd.WaitDelay = waitDelay

	var stdout = newCappedBuffer(MaxExecOutput)
	var stderr = newCappedBuffer(MaxExecOutput)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	result := runtimes.ExecResult{Stdout: stdout.buf.Bytes(), Stderr: stderr.buf.Bytes()}
	if stdout.truncated() || stderr.truncated() {
		result.Truncated = true
	}

	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return result, runtimes.ErrTimeout
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, nil
	}
	if err != nil {
		return result, fmt.Errorf("run %s: %w", c.Name, err)
	}
	return result, nil
}

func MergeEnv(extra map[string]string) []string {
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
