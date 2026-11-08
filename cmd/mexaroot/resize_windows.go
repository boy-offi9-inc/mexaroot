//go:build windows

package main

import (
	"context"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
)

func watchResize(context.Context, *client.Client, uint32, int, bool) func() {
	return func() {}
}
