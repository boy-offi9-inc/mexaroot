package keepalive

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strings"
	"sync"
	"time"
)

const (
	lockCommand    = "termux-wake-lock"
	unlockCommand  = "termux-wake-unlock"
	commandTimeout = 5 * time.Second
)

var ErrUnavailable = errors.New("wake lock command is not available")

type Lock struct {
	lockCommand   string
	unlockCommand string

	mu   sync.Mutex
	held bool
}

func New() *Lock {
	return NewWithCommands(lockCommand, unlockCommand)
}

func NewWithCommands(lock, unlock string) *Lock {
	return &Lock{lockCommand: lock, unlockCommand: unlock}
}

func (l *Lock) Held() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.held
}

func (l *Lock) Acquire() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.held {
		return nil
	}
	if err := run(l.lockCommand); err != nil {
		return fmt.Errorf("acquire wake lock: %w", err)
	}
	l.held = true
	return nil
}

func (l *Lock) Release() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	if !l.held {
		return nil
	}
	l.held = false
	if err := run(l.unlockCommand); err != nil {
		return fmt.Errorf("release wake lock: %w", err)
	}
	return nil
}

func run(name string) error {
	path, err := exec.LookPath(name)
	if err != nil {
		return ErrUnavailable
	}
	ctx, cancel := context.WithTimeout(context.Background(), commandTimeout)
	defer cancel()

	output, err := exec.CommandContext(ctx, path).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%s: %w: %s", name, err, strings.TrimSpace(string(output)))
	}
	return nil
}
