package server

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

func serverURL(t *testing.T, registry *runtimes.Registry) string {
	t.Helper()
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
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

func request(ctx context.Context, t *testing.T, conn *websocket.Conn, id, msgType string, payload any) {
	t.Helper()
	req, err := wire.NewRequest(id, msgType, payload)
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
}

func nextMessage(ctx context.Context, t *testing.T, conn *websocket.Conn) wire.Message {
	t.Helper()
	_, data, err := conn.Read(ctx)
	if err != nil {
		t.Fatalf("Read: %v", err)
	}
	msg, err := wire.Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	return msg
}

func awaitResponse(ctx context.Context, t *testing.T, conn *websocket.Conn, id string) wire.Message {
	t.Helper()
	for {
		msg := nextMessage(ctx, t, conn)
		if msg.ID == id {
			return msg
		}
	}
}

func awaitOutput(ctx context.Context, t *testing.T, conn *websocket.Conn, want string) {
	t.Helper()
	var seen strings.Builder
	for {
		msg := nextMessage(ctx, t, conn)
		if msg.Type != wire.TypeSessionOutput || msg.Stream == nil {
			continue
		}
		var out wire.SessionOutput
		if err := msg.DecodePayload(&out); err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}
		data, err := base64.StdEncoding.DecodeString(out.Data)
		if err != nil {
			t.Fatalf("DecodeString: %v", err)
		}
		seen.Write(data)
		if strings.Contains(seen.String(), want) {
			return
		}
	}
}

func startShell(ctx context.Context, t *testing.T, conn *websocket.Conn, shell string) uint32 {
	t.Helper()
	request(ctx, t, conn, "start", wire.TypeSessionStart, wire.SessionStartRequest{
		Runtime: nativeRuntimeID,
		Shell:   shell,
		Cols:    80,
		Rows:    24,
	})
	resp := awaitResponse(ctx, t, conn, "start")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("session.start failed: %+v", resp.Error)
	}
	var started wire.StreamResponse
	if err := resp.DecodePayload(&started); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if started.Stream == 0 {
		t.Fatal("expected a stream number")
	}
	return started.Stream
}

func TestSessionLifecycle(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ConPTY on Windows yet; shell sessions are documented as unavailable")
	}
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	stream := startShell(ctx, t, conn, "cat")

	request(ctx, t, conn, "input", wire.TypeSessionInput, wire.SessionInput{
		Stream: stream,
		Data:   base64.StdEncoding.EncodeToString([]byte("hello\n")),
	})
	awaitOutput(ctx, t, conn, "hello")

	request(ctx, t, conn, "close", wire.TypeSessionClose, wire.SessionClose{Stream: stream})
	var closed, exited bool
	for !closed || !exited {
		msg := nextMessage(ctx, t, conn)
		switch {
		case msg.ID == "close":
			if msg.OK == nil || !*msg.OK {
				t.Fatalf("session.close failed: %+v", msg.Error)
			}
			closed = true
		case msg.Type == wire.TypeSessionExit && msg.Stream != nil:
			if *msg.Stream != stream {
				t.Fatalf("exit for unexpected stream %d", *msg.Stream)
			}
			exited = true
		}
	}
}

func TestSessionResize(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ConPTY on Windows yet; shell sessions are documented as unavailable")
	}
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	stream := startShell(ctx, t, conn, "cat")

	request(ctx, t, conn, "resize", wire.TypeSessionResize, wire.SessionResize{Stream: stream, Cols: 120, Rows: 40})
	resp := awaitResponse(ctx, t, conn, "resize")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("session.resize failed: %+v", resp.Error)
	}
}

func TestSessionUnknownStream(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	request(ctx, t, conn, "input", wire.TypeSessionInput, wire.SessionInput{Stream: 999, Data: ""})
	expectError(t, awaitResponse(ctx, t, conn, "input"), wire.CodeNotFound)
}

func TestSessionInvalidInputData(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ConPTY on Windows yet; shell sessions are documented as unavailable")
	}
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	stream := startShell(ctx, t, conn, "cat")
	request(ctx, t, conn, "input", wire.TypeSessionInput, wire.SessionInput{Stream: stream, Data: "!!!"})
	expectError(t, awaitResponse(ctx, t, conn, "input"), wire.CodeInvalidRequest)
}

func TestSessionRequiresRuntime(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	request(ctx, t, conn, "start", wire.TypeSessionStart, wire.SessionStartRequest{Cols: 80, Rows: 24})
	expectError(t, awaitResponse(ctx, t, conn, "start"), wire.CodeInvalidRequest)
}

func TestSessionUnknownRuntime(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	request(ctx, t, conn, "start", wire.TypeSessionStart, wire.SessionStartRequest{Runtime: "missing"})
	expectError(t, awaitResponse(ctx, t, conn, "start"), wire.CodeNotFound)
}

func TestSessionUnsupportedRuntime(t *testing.T) {
	registry := runtimes.NewRegistry()
	if err := registry.Register(failingRuntime{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	ctx, conn := dial(t, serverURL(t, registry))
	authenticate(ctx, t, conn)
	request(ctx, t, conn, "start", wire.TypeSessionStart, wire.SessionStartRequest{Runtime: "failing"})
	expectError(t, awaitResponse(ctx, t, conn, "start"), wire.CodeUnsupported)
}
