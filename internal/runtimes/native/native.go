package native

import (
	"context"
	"os"
	"path/filepath"
	"runtime"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/hostexec"
)

var (
	_ runtimes.Interactive = (*Runtime)(nil)
	_ runtimes.FileRoot    = (*Runtime)(nil)
)

type Runtime struct {
	id   string
	kind runtimes.Kind
	name string
}

// isTermux reports whether the daemon runs inside Termux on Android: either
// the Go toolchain targeted android, or Termux's own environment marker is
// present. Everywhere else the native runtime is the plain host.
func isTermux() bool {
	if runtime.GOOS == "android" {
		return true
	}
	_, ok := os.LookupEnv("TERMUX_VERSION")
	return ok
}

func New() *Runtime {
	if isTermux() {
		return &Runtime{id: "termux", kind: runtimes.KindTermux, name: "Termux"}
	}
	return &Runtime{id: "host", kind: runtimes.KindHost, name: "Host"}
}

func (r *Runtime) Info() runtimes.Info {
	return runtimes.Info{
		ID:    r.id,
		Kind:  r.kind,
		Name:  r.name,
		State: runtimes.StateAvailable,
	}
}

func (r *Runtime) Exec(ctx context.Context, req runtimes.ExecRequest) (runtimes.ExecResult, error) {
	if len(req.Argv) == 0 {
		return runtimes.ExecResult{}, runtimes.ErrEmptyCommand
	}
	return hostexec.Run(ctx, hostexec.Command{
		Name:    req.Argv[0],
		Args:    req.Argv[1:],
		Dir:     req.Cwd,
		Env:     hostexec.MergeEnv(req.Env),
		Timeout: req.Timeout,
	})
}

func (r *Runtime) StartSession(_ context.Context, req runtimes.SessionRequest) (runtimes.Session, error) {
	argv := req.Argv
	if len(argv) == 0 {
		argv = []string{defaultShell()}
	}
	return hostexec.StartPTY(hostexec.Command{
		Name: argv[0],
		Args: argv[1:],
		Dir:  req.Cwd,
		Env:  hostexec.MergeEnv(req.Env),
	}, req.Cols, req.Rows)
}

// Root reports the host file system, which is what commands run in.
func (r *Runtime) Root() string {
	return string(filepath.Separator)
}

func defaultShell() string {
	if shell := os.Getenv("SHELL"); shell != "" {
		return shell
	}
	return "sh"
}
