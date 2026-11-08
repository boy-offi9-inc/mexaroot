package proroot

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/prootdistro"
)

const baselinePath = "PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin"

func requireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires a unix shell")
	}
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proroot")
	script := "#!/bin/sh\nfor arg in \"$@\"; do\n  printf '%s\\n' \"$arg\"\ndone\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func launchPrefix(rootfs, cwd string) []string {
	return []string{
		"-r", rootfs, "-0", "--link2symlink",
		"-b", "/dev", "-b", "/proc", "-b", "/sys",
		"-w", cwd, "/usr/bin/env", "-i",
	}
}

func newRuntime(t *testing.T, root string) *Runtime {
	t.Helper()
	rt, err := New("alpine", Config{Binary: fakeBinary(t), RootfsRoot: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return rt
}

func execArgs(t *testing.T, rt *Runtime, req runtimes.ExecRequest) []string {
	t.Helper()
	res, err := rt.Exec(context.Background(), req)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(res.Stdout), "\n"), "\n")
}

func TestNewValidates(t *testing.T) {
	if _, err := New("-bad", Config{RootfsRoot: "/root"}); !errors.Is(err, prootdistro.ErrInvalidAlias) {
		t.Fatalf("expected ErrInvalidAlias, got %v", err)
	}
	if _, err := New("alpine", Config{}); !errors.Is(err, ErrNoRootfs) {
		t.Fatalf("expected ErrNoRootfs, got %v", err)
	}
}

func TestInfo(t *testing.T) {
	requireUnix(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "alpine"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}

	info := newRuntime(t, root).Info()
	if info.ID != "alpine-proroot" || info.Kind != runtimes.KindProroot || info.State != runtimes.StateAvailable {
		t.Fatalf("unexpected info: %+v", info)
	}

	missingRootfs, err := New("ubuntu", Config{Binary: fakeBinary(t), RootfsRoot: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := missingRootfs.Info().State; got != runtimes.StateUnavailable {
		t.Fatalf("expected unavailable without a rootfs, got %s", got)
	}

	missingBinary, err := New("alpine", Config{Binary: filepath.Join(root, "absent"), RootfsRoot: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := missingBinary.Info().State; got != runtimes.StateUnavailable {
		t.Fatalf("expected unavailable without the launcher, got %s", got)
	}
}

func TestExecBuildsLaunchCommand(t *testing.T) {
	requireUnix(t)
	root := t.TempDir()
	rt := newRuntime(t, root)
	got := execArgs(t, rt, runtimes.ExecRequest{
		Argv: []string{"echo", "hi there"},
		Cwd:  "/srv",
		Env:  map[string]string{"A": "1"},
	})

	want := launchPrefix(filepath.Join(root, "alpine"), "/srv")
	want = append(want,
		"A=1", "HOME=/root", "LANG=C.UTF-8", baselinePath, "TERM=dumb", "TMPDIR=/tmp",
		"echo", "hi there",
	)
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExecDefaults(t *testing.T) {
	requireUnix(t)
	root := t.TempDir()
	rt := newRuntime(t, root)
	got := execArgs(t, rt, runtimes.ExecRequest{Argv: []string{"uname", "-a"}})

	want := launchPrefix(filepath.Join(root, "alpine"), "/root")
	want = append(want,
		"HOME=/root", "LANG=C.UTF-8", baselinePath, "TERM=dumb", "TMPDIR=/tmp",
		"uname", "-a",
	)
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestEnvOverridesBaseline(t *testing.T) {
	requireUnix(t)
	rt := newRuntime(t, t.TempDir())
	got := execArgs(t, rt, runtimes.ExecRequest{
		Argv: []string{"true"},
		Env:  map[string]string{"HOME": "/home/dev"},
	})
	if !slices.Contains(got, "HOME=/home/dev") || slices.Contains(got, "HOME=/root") {
		t.Fatalf("expected the request to override HOME, got %q", got)
	}
}

func TestExecRejectsInvalidArguments(t *testing.T) {
	rt, err := New("alpine", Config{Binary: "proroot", RootfsRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	cases := map[string]runtimes.ExecRequest{
		"env name with equals": {Argv: []string{"true"}, Env: map[string]string{"A=B": "x"}},
		"empty env name":       {Argv: []string{"true"}, Env: map[string]string{"": "x"}},
		"command with equals":  {Argv: []string{"a=b"}},
	}
	for name, req := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := rt.Exec(context.Background(), req)
			if !errors.Is(err, runtimes.ErrInvalidArgument) {
				t.Fatalf("expected ErrInvalidArgument, got %v", err)
			}
		})
	}
}

func TestExecEmptyArgv(t *testing.T) {
	rt, err := New("alpine", Config{Binary: "proroot", RootfsRoot: t.TempDir()})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := rt.Exec(context.Background(), runtimes.ExecRequest{}); !errors.Is(err, runtimes.ErrEmptyCommand) {
		t.Fatalf("expected ErrEmptyCommand, got %v", err)
	}
}

func TestDefaultConfigHonorsEnvironment(t *testing.T) {
	t.Setenv(binaryEnv, "/opt/proroot/bin/proroot")
	if got := DefaultConfig().Binary; got != "/opt/proroot/bin/proroot" {
		t.Fatalf("got %q", got)
	}
	t.Setenv(binaryEnv, "")
	if got := DefaultConfig().Binary; got != "proroot" {
		t.Fatalf("got %q", got)
	}
}

func TestAvailable(t *testing.T) {
	requireUnix(t)
	if !Available(Config{Binary: fakeBinary(t)}) {
		t.Fatal("expected the launcher to be available")
	}
	if Available(Config{Binary: "mexaroot-missing-launcher"}) {
		t.Fatal("expected the launcher to be unavailable")
	}
}
