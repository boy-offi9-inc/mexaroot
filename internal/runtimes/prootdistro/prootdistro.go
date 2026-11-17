package prootdistro

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/hostexec"
)

const (
	defaultBinary = "proot-distro"
	rootfsSubdir  = "var/lib/proot-distro/installed-rootfs"
)

var (
	aliasPattern     = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
	ErrInvalidAlias  = errors.New("invalid distribution alias")
	interactiveShell = []string{"/bin/sh", "-l"}
)

var (
	_ runtimes.Interactive = (*Runtime)(nil)
	_ runtimes.FileRoot    = (*Runtime)(nil)
)

type Config struct {
	Binary     string
	RootfsRoot string
}

func DefaultConfig() Config {
	cfg := Config{Binary: defaultBinary}
	if prefix := os.Getenv("PREFIX"); prefix != "" {
		cfg.RootfsRoot = filepath.Join(prefix, rootfsSubdir)
	}
	return cfg
}

type Runtime struct {
	alias string
	cfg   Config
}

func New(alias string, cfg Config) (*Runtime, error) {
	if !ValidAlias(alias) {
		return nil, fmt.Errorf("%w: %q", ErrInvalidAlias, alias)
	}
	if cfg.Binary == "" {
		cfg.Binary = defaultBinary
	}
	return &Runtime{alias: alias, cfg: cfg}, nil
}

func ValidAlias(alias string) bool {
	return aliasPattern.MatchString(alias)
}

func Discover(cfg Config) ([]*Runtime, error) {
	if cfg.RootfsRoot == "" {
		return nil, nil
	}
	entries, err := os.ReadDir(cfg.RootfsRoot)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, fmt.Errorf("read rootfs directory: %w", err)
	}

	var found []*Runtime
	for _, entry := range entries {
		info, err := os.Stat(filepath.Join(cfg.RootfsRoot, entry.Name()))
		if err != nil || !info.IsDir() {
			continue
		}
		rt, err := New(entry.Name(), cfg)
		if err != nil {
			continue
		}
		found = append(found, rt)
	}
	return found, nil
}

func (r *Runtime) Root() string {
	if r.cfg.RootfsRoot == "" {
		return ""
	}
	return filepath.Join(r.cfg.RootfsRoot, r.alias)
}

func (r *Runtime) Info() runtimes.Info {
	return runtimes.Info{
		ID:    r.alias,
		Kind:  runtimes.KindProotDistro,
		Name:  r.alias,
		State: r.state(),
	}
}

func (r *Runtime) Exec(ctx context.Context, req runtimes.ExecRequest) (runtimes.ExecResult, error) {
	if len(req.Argv) == 0 {
		return runtimes.ExecResult{}, runtimes.ErrEmptyCommand
	}
	args, err := r.loginArgs(req.Cwd, req.Env, req.Argv)
	if err != nil {
		return runtimes.ExecResult{}, err
	}
	return hostexec.Run(ctx, hostexec.Command{
		Name:    r.cfg.Binary,
		Args:    args,
		Timeout: req.Timeout,
	})
}

func (r *Runtime) StartSession(_ context.Context, req runtimes.SessionRequest) (runtimes.Session, error) {
	args, err := r.loginArgs(req.Cwd, req.Env, req.Argv)
	if err != nil {
		return nil, err
	}

	var hostEnv map[string]string
	if termName, ok := req.Env["TERM"]; ok {
		hostEnv = map[string]string{"TERM": termName}
	}
	return hostexec.StartPTY(hostexec.Command{
		Name: r.cfg.Binary,
		Args: args,
		Env:  hostexec.MergeEnv(hostEnv),
	}, req.Cols, req.Rows)
}

func (r *Runtime) state() runtimes.State {
	if _, err := exec.LookPath(r.cfg.Binary); err != nil {
		return runtimes.StateUnavailable
	}
	if r.cfg.RootfsRoot != "" {
		info, err := os.Stat(filepath.Join(r.cfg.RootfsRoot, r.alias))
		if err != nil || !info.IsDir() {
			return runtimes.StateUnavailable
		}
	}
	return runtimes.StateAvailable
}

func (r *Runtime) loginArgs(cwd string, env map[string]string, argv []string) ([]string, error) {
	assignments, err := envAssignments(env)
	if err != nil {
		return nil, err
	}

	args := []string{"login", r.alias}
	if cwd != "" {
		args = append(args, "--work-dir", cwd)
	}
	if len(argv) == 0 && len(assignments) == 0 {
		return args, nil
	}

	args = append(args, "--")
	if len(assignments) > 0 {
		args = append(args, "env")
		args = append(args, assignments...)
	}
	if len(argv) == 0 {
		argv = interactiveShell
	}
	return append(args, argv...), nil
}

func envAssignments(env map[string]string) ([]string, error) {
	keys := make([]string, 0, len(env))
	for key := range env {
		if key == "" || strings.Contains(key, "=") {
			return nil, fmt.Errorf("%w: environment variable name %q", runtimes.ErrInvalidArgument, key)
		}
		keys = append(keys, key)
	}
	sort.Strings(keys)

	assignments := make([]string, 0, len(keys))
	for _, key := range keys {
		assignments = append(assignments, key+"="+env[key])
	}
	return assignments, nil
}
