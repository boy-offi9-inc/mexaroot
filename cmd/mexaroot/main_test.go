package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

func TestHelpListsCommandsAndFlags(t *testing.T) {
	var opts options
	var out bytes.Buffer
	printHelp(&out, ui.NewStyler(false), newFlagSet(&opts))

	for _, want := range []string{"mexaroot", "runtimes", "exec", "shell", "-url", "-token", "-no-color", "EXAMPLES"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("help is missing %q", want)
		}
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Error("plain help must not contain escape sequences")
	}
}

func TestHelpWithColorContainsEscapeSequences(t *testing.T) {
	var opts options
	var out bytes.Buffer
	printHelp(&out, ui.NewStyler(true), newFlagSet(&opts))
	if !strings.Contains(out.String(), "\x1b[") {
		t.Error("colored help must contain escape sequences")
	}
}

func TestIsCommand(t *testing.T) {
	for _, name := range []string{"runtimes", "exec", "shell", "help"} {
		if !isCommand(name) {
			t.Errorf("%q must be a command", name)
		}
	}
	if isCommand("bogus") {
		t.Error("bogus must not be a command")
	}
}

func TestHintFor(t *testing.T) {
	unauthorized := &wire.Error{Code: wire.CodeUnauthorized, Message: "invalid token"}
	if hint := hintFor(unauthorized); !strings.Contains(hint, "-print-token") {
		t.Errorf("unexpected hint %q", hint)
	}
	refused := errors.New("dial tcp 127.0.0.1:8750: connect: connection refused")
	if hint := hintFor(refused); !strings.Contains(hint, "mexarootd") {
		t.Errorf("unexpected hint %q", hint)
	}
	if hint := hintFor(errors.New("something else")); hint != "" {
		t.Errorf("expected no hint, got %q", hint)
	}
}

func TestPrintErrorPlain(t *testing.T) {
	var out bytes.Buffer
	printError(&out, ui.NewStyler(false), errors.New("boom"))
	if out.String() != "mexaroot: boom\n" {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestPrintErrorStyledIncludesHint(t *testing.T) {
	var out bytes.Buffer
	printError(&out, ui.NewStyler(true), errors.New("connect: connection refused"))
	text := out.String()
	if !strings.Contains(text, "✗") || !strings.Contains(text, "mexarootd") {
		t.Fatalf("unexpected output %q", text)
	}
}

func TestMatchChoice(t *testing.T) {
	choices := []wire.RuntimeInfo{{ID: "termux"}, {ID: "ubuntu"}}
	cases := []struct {
		input string
		want  string
		ok    bool
	}{
		{"1", "termux", true},
		{" 2 ", "ubuntu", true},
		{"ubuntu", "ubuntu", true},
		{"0", "", false},
		{"3", "", false},
		{"nope", "", false},
		{"", "", false},
	}
	for _, tc := range cases {
		got, ok := matchChoice(choices, tc.input)
		if got != tc.want || ok != tc.ok {
			t.Errorf("matchChoice(%q) = %q, %v; want %q, %v", tc.input, got, ok, tc.want, tc.ok)
		}
	}
}
