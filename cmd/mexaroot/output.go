package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

func printError(w io.Writer, s ui.Styler, err error) {
	if s.Enabled() {
		fmt.Fprintf(w, "%s %v\n", s.BoldPaint(ui.Red, "✗"), err)
	} else {
		fmt.Fprintf(w, "mexaroot: %v\n", err)
	}
	if hint := hintFor(err); hint != "" {
		fmt.Fprintf(w, "  %s\n", s.Dim(hint))
	}
}

func hintFor(err error) string {
	var werr *wire.Error
	switch {
	case errors.As(err, &werr) && werr.Code == wire.CodeUnauthorized:
		return "The pairing token was rejected. Print it on the device with: mexarootd -print-token"
	case strings.Contains(err.Error(), "connection refused"):
		return "Is the daemon running? Start it on the device with: mexarootd"
	default:
		return ""
	}
}
