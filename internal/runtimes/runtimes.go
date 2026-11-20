package runtimes

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

type Kind string

const (
	KindHost        Kind = "host"
	KindTermux      Kind = "termux"
	KindProotDistro Kind = "proot-distro"
	KindProroot     Kind = "proroot"
)

type State string

const (
	StateAvailable   State = "available"
	StateUnavailable State = "unavailable"
)

type Info struct {
	ID    string
	Kind  Kind
	Name  string
	State State
}

type ExecRequest struct {
	Argv    []string
	Cwd     string
	Env     map[string]string
	Timeout time.Duration
}

type ExecResult struct {
	ExitCode int
	Stdout   []byte
	Stderr   []byte
	// Truncated is true when stdout or stderr hit MaxExecOutput; whatever
	// came after the cap was discarded, not buffered.
	Truncated bool
}

var (
	ErrEmptyCommand    = errors.New("empty command")
	ErrTimeout         = errors.New("command timed out")
	ErrNotFound        = errors.New("runtime not found")
	ErrDuplicate       = errors.New("runtime already registered")
	ErrNoID            = errors.New("runtime has no id")
	ErrInvalidArgument = errors.New("invalid argument")
)

type Runtime interface {
	Info() Info
	Exec(ctx context.Context, req ExecRequest) (ExecResult, error)
}

type Registry struct {
	mu       sync.RWMutex
	runtimes map[string]Runtime
}

func NewRegistry() *Registry {
	return &Registry{runtimes: make(map[string]Runtime)}
}

func (r *Registry) Register(rt Runtime) error {
	id := rt.Info().ID
	if id == "" {
		return ErrNoID
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.runtimes[id]; exists {
		return fmt.Errorf("%w: %s", ErrDuplicate, id)
	}
	r.runtimes[id] = rt
	return nil
}

func (r *Registry) Get(id string) (Runtime, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	rt, ok := r.runtimes[id]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, id)
	}
	return rt, nil
}

func (r *Registry) List() []Info {
	r.mu.RLock()
	defer r.mu.RUnlock()
	infos := make([]Info, 0, len(r.runtimes))
	for _, rt := range r.runtimes {
		infos = append(infos, rt.Info())
	}
	sort.Slice(infos, func(i, j int) bool { return infos[i].ID < infos[j].ID })
	return infos
}

type SessionRequest struct {
	Argv []string
	Cwd  string
	Env  map[string]string
	Cols int
	Rows int
}

type Session interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows int) error
	Close() error
	Wait() (int, error)
}

type Interactive interface {
	Runtime
	StartSession(ctx context.Context, req SessionRequest) (Session, error)
}

// FileRoot is implemented by runtimes that expose a file system for
// file transfers. Paths in file messages are relative to Root().
type FileRoot interface {
	Runtime
	Root() string
}
