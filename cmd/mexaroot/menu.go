package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const maxMenuAttempts = 3

func chooseRuntime(ctx context.Context, c *client.Client, s ui.Styler) (string, error) {
	var resp wire.RuntimeListResponse
	if err := c.Call(ctx, wire.TypeRuntimeList, nil, &resp); err != nil {
		return "", err
	}

	var choices []wire.RuntimeInfo
	for _, rt := range resp.Runtimes {
		if rt.State == "available" {
			choices = append(choices, rt)
		}
	}
	switch len(choices) {
	case 0:
		return "", errors.New("no runtimes are available")
	case 1:
		return choices[0].ID, nil
	}

	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, "  "+s.BoldPaint(ui.Cyan, "Choose a runtime"))
	fmt.Fprintln(os.Stderr)
	for i, rt := range choices {
		fmt.Fprintf(os.Stderr, "  %s  %s  %s\n",
			s.Dim(strconv.Itoa(i+1)),
			s.BoldPaint(ui.Cyan, rt.ID),
			s.Dim(rt.Kind),
		)
	}
	fmt.Fprintln(os.Stderr)

	for attempt := 0; attempt < maxMenuAttempts; attempt++ {
		fmt.Fprintf(os.Stderr, "  %s ", s.Bold(fmt.Sprintf("Select [1-%d]:", len(choices))))
		line, err := readLine(os.Stdin)
		if err != nil {
			return "", err
		}
		if id, ok := matchChoice(choices, line); ok {
			return id, nil
		}
		fmt.Fprintln(os.Stderr, "  "+s.Paint(ui.Red, "Not a valid choice."))
	}
	return "", errors.New("no runtime selected")
}

func matchChoice(choices []wire.RuntimeInfo, input string) (string, bool) {
	input = strings.TrimSpace(input)
	if n, err := strconv.Atoi(input); err == nil && n >= 1 && n <= len(choices) {
		return choices[n-1].ID, true
	}
	for _, rt := range choices {
		if rt.ID == input {
			return rt.ID, true
		}
	}
	return "", false
}

func readLine(f *os.File) (string, error) {
	var line []byte
	buf := make([]byte, 1)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				return string(line), nil
			}
			line = append(line, buf[0])
		}
		if err != nil {
			if len(line) > 0 {
				return string(line), nil
			}
			return "", err
		}
	}
}
