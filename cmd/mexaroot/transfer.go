package main

import (
	"context"
	"errors"
	"os"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
)

var errTransferUsage = errors.New("usage: mexaroot push <runtime> <local> <remote> | mexaroot pull <runtime> <remote> <local>")

func pushCommand(ctx context.Context, c *client.Client, args []string) (int, error) {
	if len(args) != 3 {
		return 2, errTransferUsage
	}
	runtimeID, source, remote := args[0], args[1], args[2]

	info, err := os.Stat(source)
	if err != nil {
		return 1, err
	}
	if info.IsDir() {
		return 1, errors.New("cannot push a directory")
	}

	if err := c.Push(ctx, runtimeID, remote, source, uint32(info.Mode().Perm())); err != nil {
		return 1, err
	}
	return 0, nil
}

func pullCommand(ctx context.Context, c *client.Client, args []string) (int, error) {
	if len(args) != 3 {
		return 2, errTransferUsage
	}
	runtimeID, remote, target := args[0], args[1], args[2]

	if err := c.Pull(ctx, runtimeID, remote, target); err != nil {
		return 1, err
	}
	return 0, nil
}
