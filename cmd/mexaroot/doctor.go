package main

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/version"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const (
	dialTimeout    = 3 * time.Second
	connectTimeout = 5 * time.Second
)

type checkStatus string

const (
	checkOK   checkStatus = "ok"
	checkWarn checkStatus = "warn"
	checkFail checkStatus = "fail"
)

type check struct {
	Name   string      `json:"name"`
	Status checkStatus `json:"status"`
	Detail string      `json:"detail"`
	Hint   string      `json:"hint,omitempty"`
}

func doctorCommand(ctx context.Context, opts options, s ui.Styler) (int, error) {
	checks := runChecks(ctx, opts)

	failures := 0
	for _, c := range checks {
		if c.Status == checkFail {
			failures++
		}
	}

	if opts.json {
		payload := map[string]any{"ok": failures == 0, "checks": checks}
		if err := writeJSON(os.Stdout, payload); err != nil {
			return 1, err
		}
	} else {
		renderChecks(os.Stdout, s, checks)
	}
	if failures > 0 {
		return 1, nil
	}
	return 0, nil
}

func runChecks(ctx context.Context, opts options) []check {
	var checks []check

	token, source, err := tokenSource(opts.token, opts.tokenFile)
	if err != nil {
		return append(checks, check{
			Name:   "Token",
			Status: checkFail,
			Detail: err.Error(),
			Hint:   "Print it on the device with: mexarootd -print-token",
		})
	}
	checks = append(checks, check{Name: "Token", Status: checkOK, Detail: "from " + source})

	host, err := dialTarget(opts.url)
	if err != nil {
		return append(checks, check{Name: "Address", Status: checkFail, Detail: err.Error()})
	}
	conn, err := net.DialTimeout("tcp", host, dialTimeout)
	if err != nil {
		return append(checks, check{
			Name:   "Address",
			Status: checkFail,
			Detail: err.Error(),
			Hint:   "Is the daemon running? Start it on the device with: mexarootd",
		})
	}
	conn.Close()
	checks = append(checks, check{Name: "Address", Status: checkOK, Detail: host + " is reachable"})

	connectCtx, cancel := context.WithTimeout(ctx, connectTimeout)
	c, err := client.Connect(connectCtx, opts.url, token, "mexaroot-doctor/"+version.Version)
	cancel()
	if err != nil {
		return append(checks, check{
			Name:   "Handshake",
			Status: checkFail,
			Detail: err.Error(),
			Hint:   hintFor(err),
		})
	}
	defer c.Close()
	checks = append(checks, check{
		Name:   "Handshake",
		Status: checkOK,
		Detail: fmt.Sprintf("daemon %s, protocol %d", c.Daemon, c.Protocol),
	})

	capabilities := strings.Join(c.Capabilities, ", ")
	if slices.Contains(c.Capabilities, wire.CapabilitySession) {
		checks = append(checks, check{Name: "Capabilities", Status: checkOK, Detail: capabilities})
	} else {
		checks = append(checks, check{
			Name:   "Capabilities",
			Status: checkWarn,
			Detail: capabilities,
			Hint:   "This daemon does not offer interactive shells.",
		})
	}

	if latency, ok := measureLatency(ctx, c); ok {
		checks = append(checks, check{
			Name:   "Latency",
			Status: checkOK,
			Detail: fmt.Sprintf("%.1f ms", float64(latency.Microseconds())/1000),
		})
	}

	var list wire.RuntimeListResponse
	if err := c.Call(ctx, wire.TypeRuntimeList, nil, &list); err != nil {
		return append(checks, check{Name: "Runtimes", Status: checkFail, Detail: err.Error()})
	}
	var unavailable []string
	for _, rt := range list.Runtimes {
		if rt.State != "available" {
			unavailable = append(unavailable, rt.ID)
		}
	}
	available := len(list.Runtimes) - len(unavailable)
	switch {
	case available == 0:
		checks = append(checks, check{
			Name:   "Runtimes",
			Status: checkFail,
			Detail: "no runtime is available",
			Hint:   "Check that proot-distro or Proroot is installed on the device.",
		})
	case len(unavailable) > 0:
		checks = append(checks, check{
			Name:   "Runtimes",
			Status: checkWarn,
			Detail: fmt.Sprintf("%d available; unavailable: %s", available, strings.Join(unavailable, ", ")),
			Hint:   "Unavailable runtimes are missing their launcher or root file system.",
		})
	default:
		checks = append(checks, check{Name: "Runtimes", Status: checkOK, Detail: fmt.Sprintf("%d available", available)})
	}
	return checks
}

func dialTarget(raw string) (string, error) {
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Host == "" {
		return "", fmt.Errorf("invalid daemon URL %q", raw)
	}
	if parsed.Port() != "" {
		return parsed.Host, nil
	}
	port := "80"
	if parsed.Scheme == "wss" || parsed.Scheme == "https" {
		port = "443"
	}
	return net.JoinHostPort(parsed.Hostname(), port), nil
}

func renderChecks(w io.Writer, s ui.Styler, checks []check) {
	rows := make([][]ui.Cell, 0, len(checks))
	failures, warnings := 0, 0
	for _, c := range checks {
		switch c.Status {
		case checkFail:
			failures++
		case checkWarn:
			warnings++
		}
		rows = append(rows, []ui.Cell{
			symbolCell(c.Status),
			{Text: c.Name, Bold: true},
			{Text: c.Detail, Dim: c.Status == checkOK},
		})
	}

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  "+s.Section("Doctor"))
	fmt.Fprintln(w)
	s.Table(w, "  ", nil, rows)

	for _, c := range checks {
		if c.Hint != "" && c.Status != checkOK {
			fmt.Fprintf(w, "\n  %s %s", s.Dim("→"), s.Dim(c.Hint))
		}
	}
	fmt.Fprintln(w)

	switch {
	case failures > 0:
		fmt.Fprintf(w, "\n  %s\n\n", s.BoldPaint(ui.Red, fmt.Sprintf("%d problem(s) found", failures)))
	case warnings > 0:
		fmt.Fprintf(w, "\n  %s\n\n", s.BoldPaint(ui.Yellow, fmt.Sprintf("Checks passed with %d warning(s)", warnings)))
	default:
		fmt.Fprintf(w, "\n  %s\n\n", s.BoldPaint(ui.Green, "All checks passed"))
	}
}

func symbolCell(status checkStatus) ui.Cell {
	switch status {
	case checkOK:
		return ui.Cell{Text: "✓", Color: ui.Green, Bold: true}
	case checkWarn:
		return ui.Cell{Text: "!", Color: ui.Yellow, Bold: true}
	default:
		return ui.Cell{Text: "✗", Color: ui.Red, Bold: true}
	}
}
