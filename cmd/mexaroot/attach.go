package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"slices"

	"golang.org/x/term"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

var errAttachUsage = errors.New("usage: mexaroot attach [stream] (run `mexaroot sessions` to list)")

// sessionsCommand lists resumable sessions on the daemon.
func sessionsCommand(ctx context.Context, c *client.Client, args []string) (int, error) {
	if len(args) > 0 {
		return 2, errors.New("usage: mexaroot sessions")
	}
	var listed wire.SessionListResponse
	if err := c.Call(ctx, wire.TypeSessionList, struct{}{}, &listed); err != nil {
		return 1, err
	}
	if len(listed.Sessions) == 0 {
		fmt.Println("no resumable sessions")
		return 0, nil
	}
	for _, s := range listed.Sessions {
		state := "attached"
		if s.Detached {
			state = "detached"
		}
		shell := s.Shell
		if shell == "" {
			shell = "default shell"
		}
		line := fmt.Sprintf("%d  %-6s %s (%s)", s.Stream, s.Runtime, shell, state)
		if s.Buffered > 0 {
			line += fmt.Sprintf(" — %d bytes replay pending", s.Buffered)
		}
		fmt.Println(line)
	}
	return 0, nil
}

// attachCommand re-attaches to a session that survived a disconnect and
// behaves like `shell` from that point on.
func attachCommand(ctx context.Context, c *client.Client, args []string, styler ui.Styler) (int, error) {
	if len(args) != 1 {
		return 2, errAttachUsage
	}
	var stream uint64
	if _, err := fmt.Sscanf(args[0], "%d", &stream); err != nil {
		return 2, errAttachUsage
	}
	if !slices.Contains(c.Capabilities, wire.CapabilitySession) {
		return 1, errors.New("the daemon does not offer interactive sessions")
	}

	fd := int(os.Stdin.Fd())
	interactive := term.IsTerminal(fd)

	req := wire.SessionAttachRequest{Stream: uint32(stream), Cols: 80, Rows: 24}
	if interactive {
		if cols, rows, err := term.GetSize(fd); err == nil {
			req.Cols, req.Rows = cols, rows
		}
	}

	var started wire.StreamResponse
	if err := c.Call(ctx, wire.TypeSessionAttach, req, &started); err != nil {
		return 1, err
	}

	if interactive {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return 1, fmt.Errorf("enter raw mode: %w", err)
		}
		defer term.Restore(fd, state)
	}

	go forwardInput(ctx, c, started.Stream)
	stopResize := watchResize(ctx, c, started.Stream, fd, interactive)
	defer stopResize()

	return pumpOutput(ctx, c, started.Stream)
}
