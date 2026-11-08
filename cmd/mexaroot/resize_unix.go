//go:build !windows

package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"golang.org/x/term"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

func watchResize(ctx context.Context, c *client.Client, stream uint32, fd int, interactive bool) func() {
	if !interactive {
		return func() {}
	}

	signals := make(chan os.Signal, 1)
	signal.Notify(signals, syscall.SIGWINCH)
	done := make(chan struct{})

	go func() {
		for {
			select {
			case <-signals:
				cols, rows, err := term.GetSize(fd)
				if err != nil {
					continue
				}
				c.Send(ctx, wire.TypeSessionResize, wire.SessionResize{
					Stream: stream,
					Cols:   cols,
					Rows:   rows,
				})
			case <-done:
				return
			}
		}
	}()

	return func() {
		signal.Stop(signals)
		close(done)
	}
}
