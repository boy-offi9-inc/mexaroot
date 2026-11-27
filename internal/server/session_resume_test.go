package server

import (
	"context"
	"encoding/base64"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

func mustServer(t *testing.T) *Server {
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
	return srv
}

func mustServerWithGrace(t *testing.T, grace time.Duration) *Server {
	t.Helper()
	srv, err := New(Config{
		Addr:        "127.0.0.1:0",
		Token:       testToken,
		Version:     "test",
		Registry:    newTestRegistry(t),
		DetachGrace: grace,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return srv
}

func serverURLFor(t *testing.T, srv *Server) string {
	t.Helper()
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return "ws" + strings.TrimPrefix(ts.URL, "http") + "/ws"
}

// TestSessionSurvivesDisconnect: a shell started on one connection keeps
// running after that connection drops, and a second connection can attach
// to it and receive output produced after the drop.
func TestSessionSurvivesDisconnect(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ConPTY on Windows yet; shell sessions are documented as unavailable")
	}
	srv := mustServer(t)
	url := serverURLFor(t, srv)
	ctx, conn := dial(t, url)
	authenticate(ctx, t, conn)
	stream := startShell(ctx, t, conn, "cat")

	// Drop the connection without closing the session.
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitDetached(t, url, stream)

	ctx2, conn2 := dial(t, url)
	authenticate(ctx2, t, conn2)

	request(ctx2, t, conn2, "list", wire.TypeSessionList, struct{}{})
	resp := awaitResponse(ctx2, t, conn2, "list")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("session.list failed: %+v", resp.Error)
	}
	var listed wire.SessionListResponse
	if err := resp.DecodePayload(&listed); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if len(listed.Sessions) != 1 {
		t.Fatalf("expected 1 resumable session, got %d", len(listed.Sessions))
	}
	info := listed.Sessions[0]
	if info.Stream != stream || !info.Detached || info.Owned {
		t.Fatalf("unexpected session info: %+v", info)
	}

	// Attach and feed input; cat echoes it back, proving the process lived.
	request(ctx2, t, conn2, "attach", wire.TypeSessionAttach, wire.SessionAttachRequest{Stream: stream})
	attachResp := awaitResponse(ctx2, t, conn2, "attach")
	if attachResp.OK == nil || !*attachResp.OK {
		t.Fatalf("session.attach failed: %+v", attachResp.Error)
	}

	request(ctx2, t, conn2, "input", wire.TypeSessionInput, wire.SessionInput{
		Stream: stream,
		Data:   base64.StdEncoding.EncodeToString([]byte("survived\n")),
	})
	awaitOutput(ctx2, t, conn2, "survived")
}

// TestSessionReplayOnAttach: output produced while detached is replayed
// before new output.
func TestSessionReplayOnAttach(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ConPTY on Windows yet; shell sessions are documented as unavailable")
	}
	srv := mustServer(t)
	url := serverURLFor(t, srv)
	ctx, conn := dial(t, url)
	authenticate(ctx, t, conn)
	stream := startShell(ctx, t, conn, "sh")

	// Start a job that keeps printing after the connection drops.
	request(ctx, t, conn, "input", wire.TypeSessionInput, wire.SessionInput{
		Stream: stream,
		Data:   base64.StdEncoding.EncodeToString([]byte("echo detached-marker; sleep 1; echo later-marker; sleep 30\n")),
	})
	awaitOutput(ctx, t, conn, "detached-marker")
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("Close: %v", err)
	}

	time.Sleep(1500 * time.Millisecond) // let later-marker accumulate in the buffer

	ctx2, conn2 := dial(t, url)
	authenticate(ctx2, t, conn2)
	request(ctx2, t, conn2, "attach", wire.TypeSessionAttach, wire.SessionAttachRequest{Stream: stream})
	attachResp := awaitResponse(ctx2, t, conn2, "attach")
	if attachResp.OK == nil || !*attachResp.OK {
		t.Fatalf("session.attach failed: %+v", attachResp.Error)
	}

	// The replay must carry both markers, in order.
	var seen strings.Builder
	deadline := time.Now().Add(10 * time.Second)
	for {
		if time.Now().After(deadline) {
			t.Fatalf("replay incomplete; saw: %q", seen.String())
		}
		msg := nextMessage(ctx2, t, conn2)
		if msg.Type != wire.TypeSessionOutput || msg.Stream == nil || *msg.Stream != stream {
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
		a := strings.Index(seen.String(), "detached-marker")
		b := strings.Index(seen.String(), "later-marker")
		if a >= 0 && b > a {
			return
		}
	}
}

// TestSessionGraceExpiry: a detached session is closed after the grace
// period and disappears from the registry.
func TestSessionGraceExpiry(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ConPTY on Windows yet; shell sessions are documented as unavailable")
	}
	srv := mustServerWithGrace(t, 1*time.Second)
	url := serverURLFor(t, srv)
	ctx, conn := dial(t, url)
	authenticate(ctx, t, conn)
	startShell(ctx, t, conn, "cat")
	if err := conn.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("Close: %v", err)
	}

	ctx2, conn2 := dial(t, url)
	authenticate(ctx2, t, conn2)

	deadline := time.Now().Add(5 * time.Second)
	for {
		request(ctx2, t, conn2, "list", wire.TypeSessionList, struct{}{})
		resp := awaitResponse(ctx2, t, conn2, "list")
		var listed wire.SessionListResponse
		if err := resp.DecodePayload(&listed); err != nil {
			t.Fatalf("DecodePayload: %v", err)
		}
		if len(listed.Sessions) == 0 {
			return // reaped
		}
		if time.Now().After(deadline) {
			t.Fatalf("session was not reaped after the grace period")
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// TestSessionAttachConflict: a second connection cannot attach to a
// session that another connection owns; after the owner drops, it can.
func TestSessionAttachConflict(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("no ConPTY on Windows yet; shell sessions are documented as unavailable")
	}
	srv := mustServer(t)
	url := serverURLFor(t, srv)
	ctx1, conn1 := dial(t, url)
	authenticate(ctx1, t, conn1)
	stream := startShell(ctx1, t, conn1, "cat")

	ctx2, conn2 := dial(t, url)
	authenticate(ctx2, t, conn2)
	request(ctx2, t, conn2, "attach", wire.TypeSessionAttach, wire.SessionAttachRequest{Stream: stream})
	expectError(t, awaitResponse(ctx2, t, conn2, "attach"), wire.CodeInvalidRequest)

	if err := conn1.Close(websocket.StatusNormalClosure, ""); err != nil {
		t.Fatalf("Close: %v", err)
	}
	waitDetached(t, url, stream)
	request(ctx2, t, conn2, "attach", wire.TypeSessionAttach, wire.SessionAttachRequest{Stream: stream})
	resp := awaitResponse(ctx2, t, conn2, "attach")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("session.attach after detach failed: %+v", resp.Error)
	}

	// And the session still echoes after reattach.
	request(ctx2, t, conn2, "input", wire.TypeSessionInput, wire.SessionInput{
		Stream: stream,
		Data:   base64.StdEncoding.EncodeToString([]byte("recovered\n")),
	})
	awaitOutput(ctx2, t, conn2, "recovered")
}

// mustReconnect dials and authenticates a throwaway connection, used to
// poll the daemon without consuming the main test connection's messages.
func mustReconnect(t *testing.T, url string) *websocket.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	t.Cleanup(cancel)
	conn, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	resp := call(ctx, t, conn, wire.TypeHello, hello(testToken, wire.Version))
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("handshake failed: %+v", resp.Error)
	}
	return conn
}

// waitDetached polls session.list until the session shows as detached.
// It uses its own throwaway connection so it does not consume messages
// queued for the test's main connection.
func waitDetached(t *testing.T, url string, stream uint32) {
	t.Helper()
	conn := mustReconnect(t, url)
	for i := 0; i < 100; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		request(ctx, t, conn, "list", wire.TypeSessionList, struct{}{})
		resp := awaitResponse(ctx, t, conn, "list")
		var listed wire.SessionListResponse
		if err := resp.DecodePayload(&listed); err != nil {
			cancel()
			t.Fatalf("DecodePayload: %v", err)
		}
		cancel()
		for _, s := range listed.Sessions {
			if s.Stream == stream && s.Detached {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("session never became detached")
}

// TestSessionListEmpty: a fresh connection lists no sessions.
func TestSessionListEmpty(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)
	request(ctx, t, conn, "list", wire.TypeSessionList, struct{}{})
	resp := awaitResponse(ctx, t, conn, "list")
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("session.list failed: %+v", resp.Error)
	}
	var listed wire.SessionListResponse
	if err := resp.DecodePayload(&listed); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if len(listed.Sessions) != 0 {
		t.Fatalf("expected no sessions, got %d", len(listed.Sessions))
	}
}
