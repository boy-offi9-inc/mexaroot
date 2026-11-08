package server

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

type failingRuntime struct {
	err error
}

func (f failingRuntime) Info() runtimes.Info {
	return runtimes.Info{
		ID:    "failing",
		Kind:  runtimes.KindTermux,
		Name:  "failing",
		State: runtimes.StateAvailable,
	}
}

func (f failingRuntime) Exec(context.Context, runtimes.ExecRequest) (runtimes.ExecResult, error) {
	return runtimes.ExecResult{}, f.err
}

func TestExecMapsInvalidArgument(t *testing.T) {
	registry := runtimes.NewRegistry()
	failing := failingRuntime{err: fmt.Errorf("%w: bad value", runtimes.ErrInvalidArgument)}
	if err := registry.Register(failing); err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Token:    testToken,
		Version:  "test",
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx, conn := dial(t, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws")
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeExecRun, wire.ExecRequest{
		Runtime: "failing",
		Argv:    []string{"anything"},
	})
	expectError(t, resp, wire.CodeInvalidRequest)
}
