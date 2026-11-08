package auth

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestGenerate(t *testing.T) {
	first, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	second, err := Generate()
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	if len(first) != tokenBytes*2 {
		t.Fatalf("unexpected token length %d", len(first))
	}
	if first == second {
		t.Fatal("tokens must be unique")
	}
}

func TestEqual(t *testing.T) {
	if !Equal("abc", "abc") {
		t.Fatal("identical tokens must be equal")
	}
	if Equal("abc", "abd") || Equal("abc", "abcd") || Equal("", "abc") {
		t.Fatal("different tokens must not be equal")
	}
}

func TestLoadOrCreateCreatesPrivateFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config", "token")
	token, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("Stat: %v", err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("unexpected mode %v", info.Mode().Perm())
		}
	}
	again, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if again != token {
		t.Fatal("second call must return the stored token")
	}
}

func TestLoadOrCreateRejectsOpenPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("permission bits are not enforced on windows")
	}
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("abc\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if err := os.Chmod(path, 0o644); err != nil {
		t.Fatalf("Chmod: %v", err)
	}
	if _, err := LoadOrCreate(path); err == nil {
		t.Fatal("expected an error for a world-readable token file")
	}
}

func TestLoadRejectsEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("expected an error for an empty token file")
	}
}
