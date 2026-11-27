package main

import (
	"os"
	"testing"
)

func TestParsePairURI(t *testing.T) {
	got, err := parsePairURI("mexaroot://pair?host=ws%3A%2F%2F127.0.0.1%3A8750&secret=abc123&name=phone")
	if err != nil {
		t.Fatalf("parsePairURI: %v", err)
	}
	if got.Host != "ws://127.0.0.1:8750" || got.Secret != "abc123" || got.Name != "phone" {
		t.Fatalf("parsed: %+v", got)
	}
	if _, err := parsePairURI("https://example.com/pair"); err == nil {
		t.Fatal("foreign scheme must be refused")
	}
	if _, err := parsePairURI("mexaroot://pair?host=ws://x"); err == nil {
		t.Fatal("missing secret must be refused")
	}
}

func TestPairEndpoint(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"ws://127.0.0.1:8750", "http://127.0.0.1:8750/pair"},
		{"wss://relay.example.com", "https://relay.example.com/pair"},
		{"127.0.0.1:8750", "http://127.0.0.1:8750/pair"},
	} {
		got, err := pairEndpoint(tc.in)
		if err != nil || got != tc.want {
			t.Errorf("pairEndpoint(%q) = %q, %v; want %q", tc.in, got, err, tc.want)
		}
	}
}

func TestSaveTokenFile(t *testing.T) {
	dir := t.TempDir()
	path := dir + "/nested/token"
	if err := saveTokenFile(path, "tok"); err != nil {
		t.Fatalf("saveTokenFile: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || string(data) != "tok\n" {
		t.Fatalf("saved: %q, %v", data, err)
	}
	info, _ := os.Stat(path)
	if info.Mode().Perm() != 0o600 {
		t.Fatalf("token mode = %o, want 600", info.Mode().Perm())
	}
}
