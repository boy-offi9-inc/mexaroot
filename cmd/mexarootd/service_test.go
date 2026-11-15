package main

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestServiceArgsDefaults(t *testing.T) {
	args, err := serviceArgs(options{addr: defaultAddr, wakeLock: true})
	if err != nil {
		t.Fatalf("serviceArgs: %v", err)
	}
	if len(args) != 0 {
		t.Fatalf("expected no arguments for the defaults, got %q", args)
	}
}

func TestServiceArgsCustom(t *testing.T) {
	dir := t.TempDir()
	tokenFile := filepath.Join(dir, "token")
	args, err := serviceArgs(options{
		addr:        "127.0.0.1:9000",
		tokenFile:   tokenFile,
		allowRemote: true,
		wakeLock:    false,
	})
	if err != nil {
		t.Fatalf("serviceArgs: %v", err)
	}
	want := []string{
		"-addr", "127.0.0.1:9000",
		"-token-file", tokenFile,
		"-allow-non-loopback",
		"-wake-lock=false",
	}
	if !slices.Equal(args, want) {
		t.Fatalf("got %q, want %q", args, want)
	}
}

func TestServiceArgsMakesTokenFileAbsolute(t *testing.T) {
	args, err := serviceArgs(options{addr: defaultAddr, tokenFile: "token", wakeLock: true})
	if err != nil {
		t.Fatalf("serviceArgs: %v", err)
	}
	if len(args) != 2 || !filepath.IsAbs(args[1]) {
		t.Fatalf("expected an absolute token path, got %q", args)
	}
}
