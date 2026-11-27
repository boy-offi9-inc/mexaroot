//go:build !windows

package hostexec

import (
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
)

const (
	defaultCols = 80
	defaultRows = 24
)

type ptySession struct {
	cmd       *exec.Cmd
	file      *os.File
	closeOnce sync.Once
}

func StartPTY(c Command, cols, rows int) (runtimes.Session, error) {
	if c.Name == "" {
		return nil, runtimes.ErrEmptyCommand
	}

	// Same contract as hostexec.Run: direct argv exec, no shell.
	cmd := exec.Command(c.Name, c.Args...) // codeql[go/command-injection] direct argv exec of the authenticated client's own request, no shell
	cmd.Dir = c.Dir
	cmd.Env = c.Env

	size := &pty.Winsize{
		Rows: dimension(rows, defaultRows),
		Cols: dimension(cols, defaultCols),
	}
	file, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return nil, fmt.Errorf("start %s: %w", c.Name, err)
	}
	return &ptySession{cmd: cmd, file: file}, nil
}

func (s *ptySession) Read(p []byte) (int, error) {
	return s.file.Read(p)
}

func (s *ptySession) Write(p []byte) (int, error) {
	return s.file.Write(p)
}

func (s *ptySession) Resize(cols, rows int) error {
	return pty.Setsize(s.file, &pty.Winsize{
		Rows: dimension(rows, defaultRows),
		Cols: dimension(cols, defaultCols),
	})
}

func (s *ptySession) Close() error {
	var err error
	s.closeOnce.Do(func() {
		if s.cmd.Process != nil {
			_ = s.cmd.Process.Kill()
		}
		err = s.file.Close()
	})
	return err
}

func (s *ptySession) Wait() (int, error) {
	err := s.cmd.Wait()
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		return exitErr.ExitCode(), nil
	}
	if err != nil {
		return -1, err
	}
	return 0, nil
}

func dimension(value, fallback int) uint16 {
	if value <= 0 || value > math.MaxUint16 {
		return uint16(fallback)
	}
	return uint16(value)
}
