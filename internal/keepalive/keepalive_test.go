package keepalive

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func requireUnix(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("requires a unix shell")
	}
}

func fakeCommand(t *testing.T, dir, name string, exitCode int) string {
	t.Helper()
	path := filepath.Join(dir, name)
	script := fmt.Sprintf("#!/bin/sh\n: > \"%s/%s.called\"\nexit %d\n", dir, name, exitCode)
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return path
}

func called(dir, name string) bool {
	_, err := os.Stat(filepath.Join(dir, name+".called"))
	return err == nil
}

func TestAcquireAndRelease(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	lock := NewWithCommands(fakeCommand(t, dir, "lock", 0), fakeCommand(t, dir, "unlock", 0))

	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !lock.Held() || !called(dir, "lock") {
		t.Fatal("expected the lock command to run and the lock to be held")
	}

	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if lock.Held() || !called(dir, "unlock") {
		t.Fatal("expected the unlock command to run and the lock to be released")
	}
}

func TestReleaseWithoutAcquireDoesNothing(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	lock := NewWithCommands(fakeCommand(t, dir, "lock", 0), fakeCommand(t, dir, "unlock", 0))

	if err := lock.Release(); err != nil {
		t.Fatalf("Release: %v", err)
	}
	if called(dir, "unlock") {
		t.Fatal("unlock must not run when nothing is held")
	}
}

func TestAcquireIsIdempotent(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	lock := NewWithCommands(fakeCommand(t, dir, "lock", 0), fakeCommand(t, dir, "unlock", 0))

	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := os.Remove(filepath.Join(dir, "lock.called")); err != nil {
		t.Fatalf("Remove: %v", err)
	}
	if err := lock.Acquire(); err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if called(dir, "lock") {
		t.Fatal("a held lock must not run the lock command again")
	}
}

func TestAcquireUnavailable(t *testing.T) {
	lock := NewWithCommands("mexaroot-missing-lock", "mexaroot-missing-unlock")
	if err := lock.Acquire(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if lock.Held() {
		t.Fatal("the lock must not be held after a failed acquire")
	}
}

func TestAcquireFailure(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	lock := NewWithCommands(fakeCommand(t, dir, "lock", 1), fakeCommand(t, dir, "unlock", 0))

	err := lock.Acquire()
	if err == nil || errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected a command failure, got %v", err)
	}
	if lock.Held() {
		t.Fatal("the lock must not be held after a failed acquire")
	}
}

func TestReleaseUnavailable(t *testing.T) {
	requireUnix(t)
	dir := t.TempDir()
	lock := NewWithCommands(fakeCommand(t, dir, "lock", 0), "mexaroot-missing-unlock")

	if err := lock.Acquire(); err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if err := lock.Release(); !errors.Is(err, ErrUnavailable) {
		t.Fatalf("expected ErrUnavailable, got %v", err)
	}
	if lock.Held() {
		t.Fatal("the lock must be marked released even when the command is missing")
	}
}
