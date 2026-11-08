package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/native"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const testToken = "test-token"

func newTestRegistry(t *testing.T) *runtimes.Registry {
	t.Helper()
	registry := runtimes.NewRegistry()
	if err := registry.Register(native.New()); err != nil {
		t.Fatalf("Register: %v", err)
	}
	return registry
}

func newTestServer(t *testing.T) string {
	t.Helper()
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Token:    testToken,
		Version:  "test",
		Registry: newTestRegistry(t),
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

func dial(t *testing.T, url string) (context.Context, *websocket.Conn) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return ctx, conn
}

func call(ctx context.Context, t *testing.T, conn *websocket.Conn, msgType string, payload any) wire.Message {
	t.Helper()
	req, err := wire.NewRequest("1", msgType, payload)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	data, err := wire.Encode(req)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("Write: %v", err)
	}
	_, reply, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	resp, err := wire.Decode(reply)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return resp
}

func hello(token string, protocol ...int) wire.HelloRequest {
	return wire.HelloRequest{Client: "test", Protocol: protocol, Token: token}
}

func authenticate(ctx context.Context, t *testing.T, conn *websocket.Conn) {
	t.Helper()
	resp := call(ctx, t, conn, wire.TypeHello, hello(testToken, wire.Version))
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("handshake failed: %+v", resp.Error)
	}
}

func expectError(t *testing.T, resp wire.Message, code string) {
	t.Helper()
	if resp.OK == nil || *resp.OK || resp.Error == nil || resp.Error.Code != code {
		t.Fatalf("expected %s error, got %+v", code, resp)
	}
}

func expectClosed(ctx context.Context, t *testing.T, conn *websocket.Conn) {
	t.Helper()
	if _, _, err := conn.Read(ctx); err == nil {
		t.Fatal("expected the connection to be closed")
	}
}

func TestHandshakeSucceeds(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	resp := call(ctx, t, conn, wire.TypeHello, hello(testToken, 1, 2))
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("handshake failed: %+v", resp.Error)
	}
	var payload wire.HelloResponse
	if err := resp.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if payload.Protocol != wire.Version || payload.Daemon != "test" {
		t.Fatalf("unexpected payload: %+v", payload)
	}
	if len(payload.Capabilities) == 0 {
		t.Fatal("expected capabilities")
	}
}

func TestHandshakeRejectsWrongToken(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	resp := call(ctx, t, conn, wire.TypeHello, hello("wrong", wire.Version))
	expectError(t, resp, wire.CodeUnauthorized)
	expectClosed(ctx, t, conn)
}

func TestHandshakeRejectsUnsupportedVersion(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	resp := call(ctx, t, conn, wire.TypeHello, hello(testToken, 99))
	expectError(t, resp, wire.CodeUnsupported)
	expectClosed(ctx, t, conn)
}

func TestRequestBeforeHandshake(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	resp := call(ctx, t, conn, wire.TypePing, nil)
	expectError(t, resp, wire.CodeUnauthorized)
	expectClosed(ctx, t, conn)
}

func TestHelloAfterHandshake(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeHello, hello(testToken, wire.Version))
	expectError(t, resp, wire.CodeInvalidRequest)
}

func TestPing(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypePing, nil)
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("ping failed: %+v", resp.Error)
	}
}

func TestRuntimeList(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeRuntimeList, nil)
	var payload wire.RuntimeListResponse
	if err := resp.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if len(payload.Runtimes) != 1 || payload.Runtimes[0].ID != "termux" {
		t.Fatalf("unexpected runtimes: %+v", payload.Runtimes)
	}
}

func TestExecRun(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeExecRun, wire.ExecRequest{
		Runtime: "termux",
		Argv:    []string{"sh", "-c", "printf hi; exit 2"},
	})
	var payload wire.ExecResponse
	if err := resp.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if payload.Stdout != "hi" || payload.ExitCode != 2 {
		t.Fatalf("unexpected result: %+v", payload)
	}
}

func TestExecUnknownRuntime(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeExecRun, wire.ExecRequest{
		Runtime: "missing",
		Argv:    []string{"true"},
	})
	expectError(t, resp, wire.CodeNotFound)
}

func TestExecInvalidRequest(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeExecRun, wire.ExecRequest{Runtime: "termux"})
	expectError(t, resp, wire.CodeInvalidRequest)
}

func TestExecTimeout(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	resp := call(ctx, t, conn, wire.TypeExecRun, wire.ExecRequest{
		Runtime:   "termux",
		Argv:      []string{"sh", "-c", "exec sleep 5"},
		TimeoutMS: 100,
	})
	expectError(t, resp, wire.CodeTimeout)
}

func TestUnsupportedMessages(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	expectError(t, call(ctx, t, conn, wire.TypeFilePush, nil), wire.CodeUnsupported)
	expectError(t, call(ctx, t, conn, "bogus.type", nil), wire.CodeUnsupported)
}

func TestNewValidatesConfig(t *testing.T) {
	registry := runtimes.NewRegistry()

	if _, err := New(Config{Addr: "127.0.0.1:8750", Registry: registry}); !errors.Is(err, ErrNoToken) {
		t.Fatalf("expected ErrNoToken, got %v", err)
	}
	if _, err := New(Config{Addr: "127.0.0.1:8750", Token: testToken}); !errors.Is(err, ErrNoRegistry) {
		t.Fatalf("expected ErrNoRegistry, got %v", err)
	}
}

func TestNewRequiresLoopback(t *testing.T) {
	registry := runtimes.NewRegistry()
	base := Config{Token: testToken, Registry: registry}

	for _, addr := range []string{"127.0.0.1:8750", "localhost:8750", "[::1]:8750"} {
		cfg := base
		cfg.Addr = addr
		if _, err := New(cfg); err != nil {
			t.Errorf("New(%q): %v", addr, err)
		}
	}
	for _, addr := range []string{"0.0.0.0:8750", ":8750", "192.168.1.10:8750"} {
		cfg := base
		cfg.Addr = addr
		if _, err := New(cfg); !errors.Is(err, ErrNonLoopback) {
			t.Errorf("New(%q): expected ErrNonLoopback, got %v", addr, err)
		}
	}
	if _, err := New(Config{Addr: "nonsense", Token: testToken, Registry: registry}); err == nil {
		t.Error("expected an error for an invalid address")
	}

	cfg := base
	cfg.Addr = "0.0.0.0:8750"
	cfg.AllowNonLoopback = true
	if _, err := New(cfg); err != nil {
		t.Errorf("New with AllowNonLoopback: %v", err)
	}
}
