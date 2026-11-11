package ui

import (
	"bytes"
	"regexp"
	"strings"
	"testing"
)

var ansiPattern = regexp.MustCompile("\x1b\\[[0-9;]*m")

func stripANSI(text string) string {
	return ansiPattern.ReplaceAllString(text, "")
}

func clearColorEnv(t *testing.T) {
	t.Helper()
	for _, name := range []string{"NO_COLOR", "FORCE_COLOR", "TERM"} {
		t.Setenv(name, "")
	}
}

func TestDisabledStylerReturnsPlainText(t *testing.T) {
	s := NewStyler(false)
	for _, got := range []string{
		s.Bold("x"),
		s.Dim("x"),
		s.Paint(Green, "x"),
		s.BoldPaint(Red, "x"),
		s.Gradient("x"),
	} {
		if got != "x" {
			t.Fatalf("expected plain text, got %q", got)
		}
	}
}

func TestEnabledStylerWrapsText(t *testing.T) {
	s := NewStyler(true)
	if got := s.Paint(Green, "ok"); got != "\x1b[32mok\x1b[0m" {
		t.Fatalf("unexpected sequence %q", got)
	}
	if got := s.BoldPaint(Red, "no"); got != "\x1b[1;31mno\x1b[0m" {
		t.Fatalf("unexpected sequence %q", got)
	}
	if got := s.Bold(""); got != "" {
		t.Fatalf("empty text must stay empty, got %q", got)
	}
	if got := s.Paint(Default, "plain"); got != "plain" {
		t.Fatalf("default color must not wrap, got %q", got)
	}
}

func TestGradientPreservesText(t *testing.T) {
	s := NewStyler(true)
	text := "◆ mexaroot"
	styled := s.Gradient(text)
	if styled == text {
		t.Fatal("expected escape sequences in the gradient")
	}
	if got := stripANSI(styled); got != text {
		t.Fatalf("got %q, want %q", got, text)
	}
}

func TestBannerContainsDetails(t *testing.T) {
	banner := stripANSI(NewStyler(true).Banner("mexaroot", "1.2.3", "a tagline"))
	for _, want := range []string{"mexaroot", "1.2.3", "a tagline"} {
		if !strings.Contains(banner, want) {
			t.Errorf("banner is missing %q: %q", want, banner)
		}
	}
}

func TestStateCell(t *testing.T) {
	cases := map[string]struct {
		text  string
		color Color
	}{
		"available":   {"● available", Green},
		"unavailable": {"○ unavailable", Red},
		"unknown":     {"· unknown", Gray},
	}
	for state, want := range cases {
		got := StateCell(state)
		if got.Text != want.text || got.Color != want.color {
			t.Errorf("StateCell(%q) = %+v", state, got)
		}
	}
}

func TestTableAlignsColumns(t *testing.T) {
	headers := []string{"id", "kind"}
	rows := [][]Cell{
		{{Text: "termux", Color: Cyan, Bold: true}, {Text: "termux"}},
		{{Text: "ubuntu", Color: Cyan, Bold: true}, {Text: "proot-distro"}},
	}
	want := "  ID      KIND\n  termux  termux\n  ubuntu  proot-distro\n"

	var plain bytes.Buffer
	NewStyler(false).Table(&plain, "  ", headers, rows)
	if plain.String() != want {
		t.Fatalf("got %q, want %q", plain.String(), want)
	}

	var styled bytes.Buffer
	NewStyler(true).Table(&styled, "  ", headers, rows)
	if got := stripANSI(styled.String()); got != want {
		t.Fatalf("styled table misaligned: got %q, want %q", got, want)
	}
}

func TestTableWithoutHeaders(t *testing.T) {
	var out bytes.Buffer
	NewStyler(false).Table(&out, "", nil, [][]Cell{
		{{Text: "a"}, {Text: "first"}, {Text: ""}},
		{{Text: "bbb"}, {Text: "second"}, {Text: "note", Dim: true}},
	})
	want := "a    first\nbbb  second  note\n"
	if out.String() != want {
		t.Fatalf("got %q, want %q", out.String(), want)
	}
}

func TestColorEnabledRules(t *testing.T) {
	clearColorEnv(t)
	if colorEnabled(nil, false) {
		t.Fatal("colors must be off without a terminal")
	}

	t.Setenv("FORCE_COLOR", "1")
	if !colorEnabled(nil, false) {
		t.Fatal("FORCE_COLOR must enable colors")
	}
	if colorEnabled(nil, true) {
		t.Fatal("an explicit no-color flag must win")
	}

	t.Setenv("NO_COLOR", "1")
	if colorEnabled(nil, false) {
		t.Fatal("NO_COLOR must win over FORCE_COLOR")
	}

	t.Setenv("NO_COLOR", "")
	t.Setenv("FORCE_COLOR", "")
	t.Setenv("TERM", "dumb")
	if colorEnabled(nil, false) {
		t.Fatal("a dumb terminal must not get colors")
	}
}

func TestIsTerminalWithoutFile(t *testing.T) {
	if IsTerminal(nil) {
		t.Fatal("nil is not a terminal")
	}
}
