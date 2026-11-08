package main

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"os"
	"slices"

	"golang.org/x/term"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const (
	stdinChunk        = 4096
	endOfTransmission = "\x04"
)

func shellCommand(ctx context.Context, c *client.Client, args []string) (int, error) {
	if len(args) < 1 || len(args) > 2 {
		return 2, errors.New("usage: mexaroot shell <runtime> [shell]")
	}
	if !slices.Contains(c.Capabilities, wire.CapabilitySession) {
		return 1, errors.New("the daemon does not offer interactive sessions")
	}

	req := wire.SessionStartRequest{Runtime: args[0], Cols: 80, Rows: 24}
	if len(args) == 2 {
		req.Shell = args[1]
	}
	if termName := os.Getenv("TERM"); termName != "" {
		req.Env = map[string]string{"TERM": termName}
	}

	fd := int(os.Stdin.Fd())
	interactive := term.IsTerminal(fd)
	if interactive {
		if cols, rows, err := term.GetSize(fd); err == nil {
			req.Cols, req.Rows = cols, rows
		}
	}

	var started wire.StreamResponse
	if err := c.Call(ctx, wire.TypeSessionStart, req, &started); err != nil {
		return 1, err
	}
	stream := started.Stream

	if interactive {
		state, err := term.MakeRaw(fd)
		if err != nil {
			return 1, fmt.Errorf("enter raw mode: %w", err)
		}
		defer term.Restore(fd, state)
	}

	go forwardInput(ctx, c, stream)
	stopResize := watchResize(ctx, c, stream, fd, interactive)
	defer stopResize()

	return pumpOutput(ctx, c, stream)
}

func forwardInput(ctx context.Context, c *client.Client, stream uint32) {
	buf := make([]byte, stdinChunk)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			sendInput(ctx, c, stream, buf[:n])
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				sendInput(ctx, c, stream, []byte(endOfTransmission))
			}
			return
		}
	}
}

func sendInput(ctx context.Context, c *client.Client, stream uint32, data []byte) {
	c.Send(ctx, wire.TypeSessionInput, wire.SessionInput{
		Stream: stream,
		Data:   base64.StdEncoding.EncodeToString(data),
	})
}

func pumpOutput(ctx context.Context, c *client.Client, stream uint32) (int, error) {
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				return 1, errors.New("connection closed")
			}
			if ev.Stream == nil || *ev.Stream != stream {
				continue
			}
			switch ev.Type {
			case wire.TypeSessionOutput:
				var out wire.SessionOutput
				if err := ev.DecodePayload(&out); err != nil {
					return 1, err
				}
				data, err := base64.StdEncoding.DecodeString(out.Data)
				if err != nil {
					return 1, err
				}
				os.Stdout.Write(data)
			case wire.TypeSessionExit:
				var exit wire.SessionExit
				if err := ev.DecodePayload(&exit); err != nil {
					return 1, err
				}
				return exit.ExitCode, nil
			}
		case <-ctx.Done():
			c.Send(context.Background(), wire.TypeSessionClose, wire.SessionClose{Stream: stream})
			return 130, nil
		}
	}
}
