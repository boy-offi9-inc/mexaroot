package proroot

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/hostexec"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/prootdistro"
)

const (
	defaultBinary = "proroot"
	binaryEnv     = "MEXAROOT_PROROOT"
	idSuffix      = "-proroot"
	defaultCwd    = "/root"
	defaultShell  = "/bin/sh"
	guestEnvPath  = "/usr/bin/env"
)

var ErrNoRootfs = errors.New("rootfs directory is not configured")

var _ runtimes.Interactive = (*Runtime)(nil)

var baselineEnv = map[string]string{
	"HOME":   "/root",
	"LANG":   "C.UTF-8",
	"PATH":   "/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin",
	"TERM":   "dumb",
	"TMPDIR": "/tmp",
}

type Config struct {
	Binary     string
	RootfsRoot string
}

func DefaultConfig() Config {
	cfg := Config{
		Binary:     defaultBinary,
		RootfsRoot: prootdistro.DefaultConfig().RootfsRoot,
	}
	if binary := os.Getenv(binaryEnv); binary != "" {
		cfg.Binary = binary
	}
	return cfg
}

func Available(cfg Config) bool {
	_, err := exec.LookPath(binaryOrDefault(cfg))
	return err == nil
}

type Runtime struct {
	alias  string
	rootfs string
	binary string
}

func New(alias string, cfg Config) (*Runtime, error) {
	if !prootdistro.ValidAlias(alias) {
		return nil, fmt.Errorf("%w: %q", prootdistro.ErrInvalidAlias, alias)
	}
	if cfg.RootfsRoot == "" {
		return nil, ErrNoRootfs
	}
	return &Runtime{
		alias:  alias,
		rootfs: filepath.Join(cfg.RootfsRoot, alias),
		binary: binaryOrDefault(cfg),
	}, nil
}

func (r *Runtime) Info() runtimes.Info {
	return runtimes.Info{
		ID:    r.alias + idSuffix,
		Kind:  runtimes.KindProroot,
		Name:  r.alias + " (proroot)",
		State: r.state(),
	}
}

func (r *Runtime) Exec(ctx context.Context, req runtimes.ExecRequest) (runtimes.ExecResult, error) {
	if len(req.Argv) == 0 {
		return runtimes.ExecResult{}, runtimes.ErrEmptyCommand
	}
	args, err := r.launchArgs(req.Cwd, req.Env, req.Argv)
	if err != nil {
		return runtimes.ExecResult{}, err
	}
	return hostexec.Run(ctx, hostexec.Command{
		Name:    r.binary,
		Args:    args,
		Timeout: req.Timeout,
	})
}

func (r *Runtime) StartSession(_ context.Context, req runtimes.SessionRequest) (runtimes.Session, error) {
	argv := req.Argv
	if len(argv) == 0 {
		argv = []string{defaultShell}
	}
	args, err := r.launchArgs(req.Cwd, req.Env, argv)
	if err != nil {
		return nil, err
	}
	return hostexec.StartPTY(hostexec.Command{
		Name: r.binary,
		Args: args,
	}, req.Cols, req.Rows)
}

func (r *Runtime) state() runtimes.State {
	if _, err := exec.LookPath(r.binary); err != nil {
		return runtimes.StateUnavailable
	}
	info, err := os.Stat(r.rootfs)
	if err != nil || !info.IsDir() {
		return runtimes.StateUnavailable
	}
	return runtimes.StateAvailable
}

func (r *Runtime) launchArgs(cwd string, env map[string]string, argv []string) ([]string, error) {
	if strings.Contains(argv[0], "=") {
		return nil, fmt.Errorf("%w: command %q must not contain '='", runtimes.ErrInvalidArgument, argv[0])
	}

	vars := make(map[string]string, len(baselineEnv)+len(env))
	for key, value := range baselineEnv {
		vars[key] = value
	}
	for key, value := range env {
		if key == "" || strings.Contains(key, "=") {
			return nil, fmt.Errorf("%w: environment variable name %q", runtimes.ErrInvalidArgument, key)
		}
		vars[key] = value
	}
	keys := make([]string, 0, len(vars))
	for key := range vars {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	if cwd == "" {
		cwd = defaultCwd
	}

	args := []string{
		"-r", r.rootfs,
		"-0",
		"--link2symlink",
		"-b", "/dev",
		"-b", "/proc",
		"-b", "/sys",
		"-w", cwd,
		guestEnvPath, "-i",
	}
	for _, key := range keys {
		args = append(args, key+"="+vars[key])
	}
	return append(args, argv...), nil
}

func binaryOrDefault(cfg Config) string {
	if cfg.Binary == "" {
		return defaultBinary
	}
	return cfg.Binary
}
