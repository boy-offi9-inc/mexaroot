package main

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/boy-offi9-inc/mexaroot/internal/service"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
)

func installServiceCommand(opts options) error {
	executable, err := os.Executable()
	if err != nil {
		return fmt.Errorf("locate executable: %w", err)
	}
	args, err := serviceArgs(opts)
	if err != nil {
		return err
	}

	dir, err := service.Install(service.Spec{
		Prefix:     os.Getenv("PREFIX"),
		Executable: executable,
		Args:       args,
	})
	if err != nil {
		return err
	}

	styler := ui.New(os.Stdout, opts.noColor)
	fmt.Println()
	fmt.Printf("  %s Service installed in %s\n\n", styler.Paint(ui.Green, "✓"), dir)
	fmt.Println("  It starts disabled. To run it under supervision:")
	fmt.Println()
	fmt.Printf("    %s %s\n", styler.Dim("1."), "pkg install termux-services, then restart Termux")
	fmt.Printf("    %s %s\n", styler.Dim("2."), styler.Bold("sv-enable mexarootd"))
	fmt.Println()
	fmt.Println("  " + styler.Dim("Logs are written to $PREFIX/var/log/sv/mexarootd/current"))
	fmt.Println()
	return nil
}

func uninstallServiceCommand(opts options) error {
	if err := service.Uninstall(os.Getenv("PREFIX")); err != nil {
		return err
	}
	styler := ui.New(os.Stdout, opts.noColor)
	fmt.Printf("\n  %s Service removed. Logs were kept.\n\n", styler.Paint(ui.Green, "✓"))
	return nil
}

func serviceArgs(opts options) ([]string, error) {
	var args []string
	if opts.addr != defaultAddr {
		args = append(args, "-addr", opts.addr)
	}
	if opts.tokenFile != "" {
		abs, err := filepath.Abs(opts.tokenFile)
		if err != nil {
			return nil, fmt.Errorf("resolve token file: %w", err)
		}
		args = append(args, "-token-file", abs)
	}
	if opts.allowRemote {
		args = append(args, "-allow-non-loopback")
	}
	if !opts.wakeLock {
		args = append(args, "-wake-lock=false")
	}
	return args, nil
}
