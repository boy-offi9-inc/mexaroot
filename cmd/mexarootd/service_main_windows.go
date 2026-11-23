//go:build windows

package main

import "os"

func mainWithError() error {
	opts, err := parseOptions(os.Args[1:])
	if err != nil {
		return err
	}
	return serve(opts)
}
