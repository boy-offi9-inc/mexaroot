//go:build windows

package hostexec

import "github.com/boy-offi9-inc/mexaroot/internal/runtimes"

func StartPTY(Command, int, int) (runtimes.Session, error) {
	return nil, ErrPTYUnsupported
}
