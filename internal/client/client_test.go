package client_test

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/native"
	"github.com/boy-offi9-inc/mexaroot/internal/server"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

var nativeRuntimeID = native.New().Info().ID

func startServer(t *testing.T, token string) string {
	t.Helper()
	registry := runtimes.NewRegistry()
	if err := registry.Register(native.New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	srv, err := server.New(server.Config{
		Addr:     "127.0.0.1:0",
		Token:    token,
		Version:  "test",
		Registry: registry,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

func TestClientEndToEnd(t *testing.T) {
	url := startServer(t, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	c, err := client.Connect(ctx, url, "secret", "test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	if len(c.Capabilities) == 0 {
		t.Fatal("expected negotiated capabilities")
	}

	var list wire.RuntimeListResponse
	if err := c.Call(ctx, wire.TypeRuntimeList, nil, &list); err != nil {
		t.Fatalf("runtime.list: %v", err)
	}
	if len(list.Runtimes) != 1 || list.Runtimes[0].ID != nativeRuntimeID {
		t.Fatalf("unexpected runtimes: %+v", list.Runtimes)
	}

	var result wire.ExecResponse
	err = c.Call(ctx, wire.TypeExecRun, wire.ExecRequest{
		Runtime: nativeRuntimeID,
		Argv:    []string{"sh", "-c", "printf ok"},
	}, &result)
	if err != nil {
		t.Fatalf("exec.run: %v", err)
	}
	if result.Stdout != "ok" || result.ExitCode != 0 {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestClientRejectsWrongToken(t *testing.T) {
	url := startServer(t, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	_, err := client.Connect(ctx, url, "wrong", "test")
	var werr *wire.Error
	if !errors.As(err, &werr) || werr.Code != wire.CodeUnauthorized {
		t.Fatalf("expected unauthorized error, got %v", err)
	}
}
