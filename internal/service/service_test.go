package service

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func readFile(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	return string(data)
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestInstallWritesScripts(t *testing.T) {
	prefix := t.TempDir()
	executable := filepath.Join(prefix, "bin", "mexarootd")

	dir, err := Install(Spec{
		Prefix:     prefix,
		Executable: executable,
		Args:       []string{"-addr", "127.0.0.1:9000"},
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	if want := filepath.Join(prefix, "var", "service", "mexarootd"); dir != want {
		t.Fatalf("got %q, want %q", dir, want)
	}

	shell := filepath.Join(prefix, "bin", "sh")
	wantRun := fmt.Sprintf("#!%s\nexec 2>&1\nexec '%s' '-addr' '127.0.0.1:9000'\n", shell, executable)
	if got := readFile(t, filepath.Join(dir, "run")); got != wantRun {
		t.Fatalf("run script:\n%q\nwant:\n%q", got, wantRun)
	}

	svlogd := filepath.Join(prefix, "bin", "svlogd")
	logDir := filepath.Join(prefix, "var", "log", "sv", "mexarootd")
	wantLog := fmt.Sprintf("#!%s\nexec '%s' -tt '%s'\n", shell, svlogd, logDir)
	if got := readFile(t, filepath.Join(dir, "log", "run")); got != wantLog {
		t.Fatalf("log script:\n%q\nwant:\n%q", got, wantLog)
	}

	if info, err := os.Stat(logDir); err != nil || !info.IsDir() {
		t.Fatalf("expected the log directory to exist: %v", err)
	}
}

func TestInstallSetsExecutableBits(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on windows")
	}
	prefix := t.TempDir()
	dir, err := Install(Spec{Prefix: prefix, Executable: filepath.Join(prefix, "bin", "mexarootd")})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	for _, path := range []string{filepath.Join(dir, "run"), filepath.Join(dir, "log", "run")} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != 0o755 {
			t.Errorf("%s has mode %v", path, info.Mode().Perm())
		}
	}
}

func TestInstallStartsDisabledOnlyOnce(t *testing.T) {
	prefix := t.TempDir()
	spec := Spec{Prefix: prefix, Executable: filepath.Join(prefix, "bin", "mexarootd")}

	dir, err := Install(spec)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	down := filepath.Join(dir, "down")
	if !exists(down) {
		t.Fatal("a new service must start disabled")
	}

	if err := os.Remove(down); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if _, err := Install(spec); err != nil {
		t.Fatalf("second Install: %v", err)
	}
	if exists(down) {
		t.Fatal("reinstalling must not disable an enabled service")
	}
}

func TestInstallQuotesArguments(t *testing.T) {
	prefix := t.TempDir()
	dir, err := Install(Spec{
		Prefix:     prefix,
		Executable: filepath.Join(prefix, "bin", "mexarootd"),
		Args:       []string{"-token-file", "/home/o'brien/my token"},
	})
	if err != nil {
		t.Fatalf("Install: %v", err)
	}
	want := `'-token-file' '/home/o'\''brien/my token'`
	if got := readFile(t, filepath.Join(dir, "run")); !strings.Contains(got, want) {
		t.Fatalf("run script does not contain %q:\n%s", want, got)
	}
}

func TestInstallValidatesInput(t *testing.T) {
	if _, err := Install(Spec{Executable: "/bin/mexarootd"}); !errors.Is(err, ErrNoPrefix) {
		t.Fatalf("expected ErrNoPrefix, got %v", err)
	}
	if _, err := Install(Spec{Prefix: t.TempDir(), Executable: "mexarootd"}); !errors.Is(err, ErrRelativeExecutable) {
		t.Fatalf("expected ErrRelativeExecutable, got %v", err)
	}
}

func TestUninstall(t *testing.T) {
	prefix := t.TempDir()
	spec := Spec{Prefix: prefix, Executable: filepath.Join(prefix, "bin", "mexarootd")}
	dir, err := Install(spec)
	if err != nil {
		t.Fatalf("Install: %v", err)
	}

	down := filepath.Join(dir, "down")
	if err := os.Remove(down); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := Uninstall(prefix); !errors.Is(err, ErrServiceEnabled) {
		t.Fatalf("expected ErrServiceEnabled, got %v", err)
	}
	if !exists(dir) {
		t.Fatal("an enabled service must not be removed")
	}

	if err := os.WriteFile(down, nil, 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := Uninstall(prefix); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if exists(dir) {
		t.Fatal("expected the service directory to be removed")
	}
	if !exists(LogDir(prefix)) {
		t.Fatal("the log directory must be kept")
	}
}

func TestUninstallMissingService(t *testing.T) {
	if err := Uninstall(t.TempDir()); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if err := Uninstall(""); !errors.Is(err, ErrNoPrefix) {
		t.Fatalf("expected ErrNoPrefix, got %v", err)
	}
}
