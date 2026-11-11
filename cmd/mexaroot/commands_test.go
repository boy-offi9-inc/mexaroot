package main

import (
	"bytes"
	"context"
	"errors"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/native"
	"github.com/boy-offi9-inc/mexaroot/internal/server"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

func startTestServer(t *testing.T, token string) string {
	t.Helper()
	registry := runtimes.NewRegistry()
	if err := registry.Register(native.New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv, err := server.New(server.Config{
		Addr:     "127.0.0.1:0",
		Token:    token,
		Version:  "test",
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

func TestCollectStatus(t *testing.T) {
	address := startTestServer(t, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := client.Connect(ctx, address, "secret", "test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	report, err := collectStatus(ctx, c, address)
	if err != nil {
		t.Fatalf("collectStatus: %v", err)
	}
	if report.Daemon != "test" || report.Protocol != wire.Version || report.URL != address {
		t.Fatalf("unexpected report: %+v", report)
	}
	if report.Runtimes.Total != 1 || report.Runtimes.Available != 1 {
		t.Fatalf("unexpected runtime counts: %+v", report.Runtimes)
	}
	if report.LatencyMS == nil {
		t.Fatal("expected a latency measurement")
	}
	if !slices.Contains(report.Capabilities, wire.CapabilityExec) {
		t.Fatalf("unexpected capabilities: %v", report.Capabilities)
	}
}

func TestRenderStatusPlain(t *testing.T) {
	latency := 1.5
	var out bytes.Buffer
	renderStatus(&out, ui.NewStyler(false), statusReport{
		URL:          "ws://127.0.0.1:8750/ws",
		Daemon:       "1.0.0",
		Protocol:     1,
		Capabilities: []string{"exec", "session"},
		Runtimes:     runtimeCounts{Total: 4, Available: 3},
		LatencyMS:    &latency,
	})
	for _, want := range []string{"STATUS", "Daemon", "1.0.0", "protocol 1", "1.5 ms", "exec  session", "3 available", "of 4"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("status output is missing %q: %q", want, out.String())
		}
	}
}

func TestRunChecksHealthy(t *testing.T) {
	address := startTestServer(t, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	checks := runChecks(ctx, options{url: address, token: "secret"})
	names := make([]string, 0, len(checks))
	for _, c := range checks {
		if c.Status == checkFail {
			t.Errorf("unexpected failure: %+v", c)
		}
		names = append(names, c.Name)
	}
	for _, want := range []string{"Token", "Address", "Handshake", "Capabilities", "Runtimes"} {
		if !slices.Contains(names, want) {
			t.Errorf("missing check %q in %v", want, names)
		}
	}
}

func TestRunChecksWrongToken(t *testing.T) {
	address := startTestServer(t, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	checks := runChecks(ctx, options{url: address, token: "wrong"})
	last := checks[len(checks)-1]
	if last.Name != "Handshake" || last.Status != checkFail || !strings.Contains(last.Hint, "-print-token") {
		t.Fatalf("unexpected final check: %+v", last)
	}
}

func TestRunChecksUnreachable(t *testing.T) {
	checks := runChecks(context.Background(), options{url: "ws://127.0.0.1:1/ws", token: "x"})
	last := checks[len(checks)-1]
	if last.Name != "Address" || last.Status != checkFail || !strings.Contains(last.Hint, "mexarootd") {
		t.Fatalf("unexpected final check: %+v", last)
	}
}

func TestRunChecksMissingToken(t *testing.T) {
	t.Setenv("MEXAROOT_TOKEN", "")
	checks := runChecks(context.Background(), options{
		url:       "ws://127.0.0.1:8750/ws",
		tokenFile: filepath.Join(t.TempDir(), "missing"),
	})
	if len(checks) != 1 || checks[0].Name != "Token" || checks[0].Status != checkFail {
		t.Fatalf("unexpected checks: %+v", checks)
	}
}

func TestRenderChecksPlain(t *testing.T) {
	var out bytes.Buffer
	renderChecks(&out, ui.NewStyler(false), []check{
		{Name: "Token", Status: checkOK, Detail: "from file"},
		{Name: "Runtimes", Status: checkWarn, Detail: "1 available", Hint: "something is missing"},
		{Name: "Handshake", Status: checkFail, Detail: "refused", Hint: "start the daemon"},
	})
	for _, want := range []string{"DOCTOR", "✓", "!", "✗", "something is missing", "start the daemon", "1 problem(s) found"} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output is missing %q: %q", want, out.String())
		}
	}

	out.Reset()
	renderChecks(&out, ui.NewStyler(false), []check{{Name: "Token", Status: checkOK, Detail: "ok"}})
	if !strings.Contains(out.String(), "All checks passed") {
		t.Errorf("unexpected summary: %q", out.String())
	}
}

func TestDialTarget(t *testing.T) {
	cases := map[string]string{
		"ws://127.0.0.1:8750/ws": "127.0.0.1:8750",
		"ws://example.com/ws":    "example.com:80",
		"wss://example.com/ws":   "example.com:443",
	}
	for raw, want := range cases {
		got, err := dialTarget(raw)
		if err != nil || got != want {
			t.Errorf("dialTarget(%q) = %q, %v; want %q", raw, got, err, want)
		}
	}
	if _, err := dialTarget("not a url"); err == nil {
		t.Error("expected an error for an invalid URL")
	}
}

func TestTokenSourcePrecedence(t *testing.T) {
	file := filepath.Join(t.TempDir(), "token")
	if err := os.WriteFile(file, []byte("from-file\n"), 0o600); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}

	t.Setenv("MEXAROOT_TOKEN", "from-env")
	if token, source, err := tokenSource("from-flag", file); err != nil || token != "from-flag" || !strings.Contains(source, "-token") {
		t.Errorf("flag: %q, %q, %v", token, source, err)
	}
	if token, source, err := tokenSource("", file); err != nil || token != "from-env" || source != "MEXAROOT_TOKEN" {
		t.Errorf("env: %q, %q, %v", token, source, err)
	}

	t.Setenv("MEXAROOT_TOKEN", "")
	if token, source, err := tokenSource("", file); err != nil || token != "from-file" || source != file {
		t.Errorf("file: %q, %q, %v", token, source, err)
	}
}

func TestWriteJSON(t *testing.T) {
	var out bytes.Buffer
	if err := writeJSON(&out, map[string]int{"a": 1}); err != nil {
		t.Fatalf("writeJSON: %v", err)
	}
	if out.String() != "{\n  \"a\": 1\n}\n" {
		t.Fatalf("unexpected output %q", out.String())
	}
}

func TestPrintJSONError(t *testing.T) {
	var out bytes.Buffer
	printJSONError(&out, &wire.Error{Code: wire.CodeUnauthorized, Message: "invalid token"})
	for _, want := range []string{`"error"`, `"code": "unauthorized"`, `"message": "unauthorized: invalid token"`} {
		if !strings.Contains(out.String(), want) {
			t.Errorf("output is missing %q: %q", want, out.String())
		}
	}

	out.Reset()
	printJSONError(&out, errors.New("boom"))
	if strings.Contains(out.String(), `"code"`) {
		t.Errorf("a plain error must not carry a code: %q", out.String())
	}
}

func TestCompletionScripts(t *testing.T) {
	var opts options
	fs := newFlagSet(&opts)

	expectations := map[string][]string{
		"bash": {"complete -F _mexaroot mexaroot", "runtimes", "doctor", "-json", "bash zsh fish"},
		"zsh":  {"#compdef mexaroot", "'status:", "-no-color[disable colored output]", "compdef _mexaroot mexaroot"},
		"fish": {"complete -c mexaroot -f", "-a 'shell'", "-o json", "__fish_seen_subcommand_from exec shell"},
	}
	for shell, wants := range expectations {
		script, err := completionScript(shell, fs)
		if err != nil {
			t.Fatalf("completionScript(%q): %v", shell, err)
		}
		for _, want := range wants {
			if !strings.Contains(script, want) {
				t.Errorf("%s completion is missing %q", shell, want)
			}
		}
	}

	if _, err := completionScript("powershell", fs); err == nil {
		t.Error("expected an error for an unsupported shell")
	}
}

func TestZshEscape(t *testing.T) {
	if got := zshEscape("a:b [c] it's"); got != `a\:b \[c\] it'\''s` {
		t.Fatalf("unexpected escape %q", got)
	}
}
