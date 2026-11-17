package native

import (
	"context"
	"os"
	"path/filepath"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/hostexec"
)

var (
	_ runtimes.Interactive = (*Runtime)(nil)
	_ runtimes.FileRoot    = (*Runtime)(nil)
)

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
