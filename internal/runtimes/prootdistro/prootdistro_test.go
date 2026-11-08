package prootdistro

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
)

func requireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires a unix shell")
	}
}

func fakeBinary(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "proot-distro")
	script := "#!/bin/sh\nfor arg in \"$@\"; do\n  printf '%s\\n' \"$arg\"\ndone\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func execArgs(t *testing.T, rt *Runtime, req runtimes.ExecRequest) []string {
	t.Helper()
	res, err := rt.Exec(context.Background(), req)
	if err != nil {
		t.Fatalf("Exec: %v", err)
	}
	return strings.Split(strings.TrimSuffix(string(res.Stdout), "\n"), "\n")
}

func TestNewValidatesAlias(t *testing.T) {
	for _, alias := range []string{"ubuntu", "ubuntu-20.04", "ubuntu_box86"} {
		if _, err := New(alias, Config{}); err != nil {
			t.Errorf("New(%q): %v", alias, err)
		}
	}
	for _, alias := range []string{"", "-login", "a/b", "../x", "a b", ".hidden"} {
		if _, err := New(alias, Config{}); !errors.Is(err, ErrInvalidAlias) {
			t.Errorf("New(%q): expected ErrInvalidAlias, got %v", alias, err)
		}
	}
}

func TestExecBuildsLoginCommand(t *testing.T) {
	requireUnix(t)
	rt, err := New("alpine", Config{Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := execArgs(t, rt, runtimes.ExecRequest{
		Argv: []string{"echo", "hi there"},
		Cwd:  "/root",
		Env:  map[string]string{"B": "2", "A": "1"},
	})
	want := []string{"login", "alpine", "--work-dir", "/root", "--", "env", "A=1", "B=2", "echo", "hi there"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExecMinimalCommand(t *testing.T) {
	requireUnix(t)
	rt, err := New("alpine", Config{Binary: fakeBinary(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	got := execArgs(t, rt, runtimes.ExecRequest{Argv: []string{"uname", "-a"}})
	want := []string{"login", "alpine", "--", "uname", "-a"}
	if !slices.Equal(got, want) {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestExecRejectsInvalidEnv(t *testing.T) {
	rt, err := New("alpine", Config{Binary: "proot-distro"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = rt.Exec(context.Background(), runtimes.ExecRequest{
		Argv: []string{"true"},
		Env:  map[string]string{"A=B": "x"},
	})
	if !errors.Is(err, runtimes.ErrInvalidArgument) {
		t.Fatalf("expected ErrInvalidArgument, got %v", err)
	}
}

func TestExecEmptyArgv(t *testing.T) {
	rt, err := New("alpine", Config{Binary: "proot-distro"})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	_, err = rt.Exec(context.Background(), runtimes.ExecRequest{})
	if !errors.Is(err, runtimes.ErrEmptyCommand) {
		t.Fatalf("expected ErrEmptyCommand, got %v", err)
	}
}

func TestInfoState(t *testing.T) {
	requireUnix(t)
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "alpine"), 0o755); err != nil {
		t.Fatalf("Mkdir: %v", err)
	}
	cfg := Config{Binary: fakeBinary(t), RootfsRoot: root}

	installed, err := New("alpine", cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := installed.Info(); got.State != runtimes.StateAvailable || got.Kind != runtimes.KindProotDistro || got.ID != "alpine" {
		t.Fatalf("unexpected info: %+v", got)
	}

	absent, err := New("ubuntu", cfg)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := absent.Info().State; got != runtimes.StateUnavailable {
		t.Fatalf("expected unavailable for a missing rootfs, got %s", got)
	}

	noBinary, err := New("alpine", Config{Binary: filepath.Join(root, "absent"), RootfsRoot: root})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if got := noBinary.Info().State; got != runtimes.StateUnavailable {
		t.Fatalf("expected unavailable without the binary, got %s", got)
	}
}

func TestDiscover(t *testing.T) {
	requireUnix(t)
	root := t.TempDir()
	for _, name := range []string{"alpine", "ubuntu", "bad name"} {
		if err := os.Mkdir(filepath.Join(root, name), 0o755); err != nil {
			t.Fatalf("Mkdir: %v", err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "notes.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Symlink("alpine", filepath.Join(root, "linked")); err != nil {
		t.Fatalf("Symlink: %v", err)
	}

	found, err := Discover(Config{RootfsRoot: root})
	if err != nil {
		t.Fatalf("Discover: %v", err)
	}
	var ids []string
	for _, rt := range found {
		ids = append(ids, rt.Info().ID)
	}
	want := []string{"alpine", "linked", "ubuntu"}
	if !slices.Equal(ids, want) {
		t.Fatalf("got %q, want %q", ids, want)
	}
}

func TestDiscoverWithoutRootfs(t *testing.T) {
	found, err := Discover(Config{RootfsRoot: filepath.Join(t.TempDir(), "missing")})
	if err != nil || len(found) != 0 {
		t.Fatalf("expected no runtimes for a missing directory, got %v, %v", found, err)
	}
	found, err = Discover(Config{})
	if err != nil || len(found) != 0 {
		t.Fatalf("expected no runtimes without a root, got %v, %v", found, err)
	}
}

func TestDefaultConfigUsesPrefix(t *testing.T) {
	t.Setenv("PREFIX", "/data/prefix")
	want := filepath.Join("/data/prefix", "var/lib/proot-distro/installed-rootfs")
	if got := DefaultConfig().RootfsRoot; got != want {
		t.Fatalf("got %q, want %q", got, want)
	}

	t.Setenv("PREFIX", "")
	if got := DefaultConfig().RootfsRoot; got != "" {
		t.Fatalf("expected no rootfs root without PREFIX, got %q", got)
	}
}
