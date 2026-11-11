package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const pingSamples = 3

type runtimeCounts struct {
	Total     int `json:"total"`
	Available int `json:"available"`
}

type statusReport struct {
	URL          string        `json:"url"`
	Daemon       string        `json:"daemon"`
	Protocol     int           `json:"protocol"`
	Capabilities []string      `json:"capabilities"`
	Runtimes     runtimeCounts `json:"runtimes"`
	LatencyMS    *float64      `json:"latency_ms,omitempty"`
}

func statusCommand(ctx context.Context, c *client.Client, opts options, s ui.Styler) (int, error) {
	report, err := collectStatus(ctx, c, opts.url)
	if err != nil {
		return 1, err
	}
	if opts.json {
		return 0, writeJSON(os.Stdout, report)
	}
	renderStatus(os.Stdout, s, report)
	return 0, nil
}

func collectStatus(ctx context.Context, c *client.Client, address string) (statusReport, error) {
	report := statusReport{
		URL:          address,
		Daemon:       c.Daemon,
		Protocol:     c.Protocol,
		Capabilities: c.Capabilities,
	}

	var list wire.RuntimeListResponse
	if err := c.Call(ctx, wire.TypeRuntimeList, nil, &list); err != nil {
		return report, err
	}
	report.Runtimes.Total = len(list.Runtimes)
	for _, rt := range list.Runtimes {
		if rt.State == "available" {
			report.Runtimes.Available++
		}
	}

	if latency, ok := measureLatency(ctx, c); ok {
		ms := float64(latency.Microseconds()) / 1000
		report.LatencyMS = &ms
	}
	return report, nil
}

func measureLatency(ctx context.Context, c *client.Client) (time.Duration, bool) {
	if !slices.Contains(c.Capabilities, wire.CapabilityKeepalive) {
		return 0, false
	}
	var best time.Duration
	for i := 0; i < pingSamples; i++ {
		start := time.Now()
		if err := c.Call(ctx, wire.TypePing, nil, nil); err != nil {
			return 0, false
		}
		if elapsed := time.Since(start); i == 0 || elapsed < best {
			best = elapsed
		}
	}
	return best, true
}

func renderStatus(w io.Writer, s ui.Styler, report statusReport) {
	rows := [][]ui.Cell{
		{{Text: "Daemon", Dim: true}, {Text: report.Daemon, Bold: true}, {Text: fmt.Sprintf("protocol %d", report.Protocol), Dim: true}},
		{{Text: "Address", Dim: true}, {Text: report.URL}},
	}
	if report.LatencyMS != nil {
		rows = append(rows, []ui.Cell{
			{Text: "Latency", Dim: true},
			{Text: fmt.Sprintf("%.1f ms", *report.LatencyMS)},
		})
	}
	rows = append(rows,
		[]ui.Cell{
			{Text: "Capabilities", Dim: true},
			{Text: strings.Join(report.Capabilities, "  "), Color: ui.Cyan},
		},
		[]ui.Cell{
			{Text: "Runtimes", Dim: true},
			{Text: fmt.Sprintf("%d available", report.Runtimes.Available), Color: ui.Green},
			{Text: fmt.Sprintf("of %d", report.Runtimes.Total), Dim: true},
		},
	)

	fmt.Fprintln(w)
	fmt.Fprintln(w, "  "+s.Section("Status"))
	fmt.Fprintln(w)
	s.Table(w, "  ", nil, rows)
	fmt.Fprintln(w)
}
