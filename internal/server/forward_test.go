package server

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

// startEcho starts a TCP echo server; every byte received is sent back.
func startEcho(t *testing.T) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_, _ = io.Copy(conn, conn)
			}()
		}
	}()
	return ln.Addr().String()
}

func forwardOpen(ctx context.Context, t *testing.T, conn *websocket.Conn, host string, port int) wire.Message {
	t.Helper()
	return call(ctx, t, conn, wire.TypeForwardOpen, wire.ForwardOpenRequest{Host: host, Port: port})
}

func TestForwardEchoRoundTrip(t *testing.T) {
	echo := startEcho(t)
	host, portStr, _ := net.SplitHostPort(echo)
	port := atoi(portStr)

	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)

	resp := forwardOpen(ctx, t, conn, host, port)
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("forward.open failed: %+v", resp.Error)
	}
	var open wire.ForwardOpenResponse
	if err := resp.DecodePayload(&open); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}

	payload := []byte("ping-forward-12345")
	req, err := wire.NewRequest("fwd1", wire.TypeForwardData, wire.SessionOutput{Data: base64.StdEncoding.EncodeToString(payload)})
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	req.Stream = &open.Stream
	data, _ := wire.Encode(req)
	if err := conn.Write(ctx, websocket.MessageText, data); err != nil {
		t.Fatalf("Write: %v", err)
	}

	// Read events until we get the echoed data (an ack for fwd1 may interleave).
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			t.Fatalf("Read: %v", err)
		}
		msg, err := wire.Decode(raw)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if msg.Type == wire.TypeForwardData && msg.Stream != nil && *msg.Stream == open.Stream {
			var out wire.SessionOutput
			if err := msg.DecodePayload(&out); err != nil {
				t.Fatalf("DecodePayload: %v", err)
			}
			got, _ := base64.StdEncoding.DecodeString(out.Data)
			if string(got) != string(payload) {
				t.Fatalf("echo mismatch: got %q", got)
			}
			return
		}
	}
	t.Fatal("timed out waiting for echoed data")
}

func TestForwardRejectsNonLoopback(t *testing.T) {
	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)

	resp := forwardOpen(ctx, t, conn, "example.com", 80)
	expectError(t, resp, wire.CodeUnsupported)

	resp = forwardOpen(ctx, t, conn, "10.1.2.3", 80)
	expectError(t, resp, wire.CodeUnsupported)

	resp = forwardOpen(ctx, t, conn, "127.0.0.1", 0)
	expectError(t, resp, wire.CodeInvalidRequest)
}

func TestForwardAllowlistPermitsOtherHosts(t *testing.T) {
	echo := startEcho(t)
	host, portStr, _ := net.SplitHostPort(echo)
	port := atoi(portStr)

	srv, err := New(Config{
		Addr:         "127.0.0.1:0",
		Token:        testToken,
		Version:      "test",
		Registry:     newTestRegistry(t),
		ForwardAllow: []string{host},
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)

	ctx, conn := dial(t, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws")
	authenticate(ctx, t, conn)

	resp := forwardOpen(ctx, t, conn, host, port)
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("forward.open failed: %+v", resp.Error)
	}
}

func TestForwardCloseEndsChannel(t *testing.T) {
	echo := startEcho(t)
	host, portStr, _ := net.SplitHostPort(echo)

	ctx, conn := dial(t, newTestServer(t))
	authenticate(ctx, t, conn)

	resp := forwardOpen(ctx, t, conn, host, atoi(portStr))
	if resp.OK == nil || !*resp.OK {
		t.Fatalf("forward.open failed: %+v", resp.Error)
	}
	var open wire.ForwardOpenResponse
	_ = resp.DecodePayload(&open)

	closeReq, _ := wire.NewRequest("c1", wire.TypeForwardClose, wire.ForwardClose{Stream: open.Stream})
	closeReq.Stream = &open.Stream
	cdata, _ := wire.Encode(closeReq)
	if err := conn.Write(ctx, websocket.MessageText, cdata); err != nil {
		t.Fatalf("Write: %v", err)
	}
	// The daemon answers the close and pushes forward.closed from the pump goroutine.
	deadline := time.Now().Add(5 * time.Second)
	sawAck, sawClosed := false, false
	for time.Now().Before(deadline) && (!sawAck || !sawClosed) {
		_, raw, err := conn.Read(ctx)
		if err != nil {
			break
		}
		msg, err := wire.Decode(raw)
		if err != nil {
			continue
		}
		if msg.ID == "c1" && msg.OK != nil {
			sawAck = true
		}
		if msg.Type == wire.TypeForwardClosed && msg.Stream != nil && *msg.Stream == open.Stream {
			sawClosed = true
		}
	}
	if !sawAck || !sawClosed {
		t.Fatalf("close incomplete: ack=%v closed=%v", sawAck, sawClosed)
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		if c < '0' || c > '9' {
			return n
		}
		n = n*10 + int(c-'0')
	}
	return n
}
