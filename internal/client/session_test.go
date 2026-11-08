package client_test

import (
	"context"
	"encoding/base64"
	"strings"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

func awaitOutput(ctx context.Context, t *testing.T, c *client.Client, want string) {
	t.Helper()
	var seen strings.Builder
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				t.Fatalf("connection closed before %q appeared; saw %q", want, seen.String())
			}
			if ev.Type != wire.TypeSessionOutput {
				continue
			}
			var out wire.SessionOutput
			if err := ev.DecodePayload(&out); err != nil {
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
		case <-ctx.Done():
			t.Fatalf("timed out waiting for %q; saw %q", want, seen.String())
		}
	}
}

func awaitExit(ctx context.Context, t *testing.T, c *client.Client) {
	t.Helper()
	for {
		select {
		case ev, ok := <-c.Events():
			if !ok {
				t.Fatal("connection closed before the session exited")
			}
			if ev.Type == wire.TypeSessionExit {
				return
			}
		case <-ctx.Done():
			t.Fatal("timed out waiting for the session to exit")
		}
	}
}

func TestClientSession(t *testing.T) {
	url := startServer(t, "secret")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	c, err := client.Connect(ctx, url, "secret", "test")
	if err != nil {
		t.Fatalf("Connect: %v", err)
	}
	defer c.Close()

	var started wire.StreamResponse
	err = c.Call(ctx, wire.TypeSessionStart, wire.SessionStartRequest{
		Runtime: "termux",
		Shell:   "cat",
		Cols:    80,
		Rows:    24,
	}, &started)
	if err != nil {
		t.Fatalf("session.start: %v", err)
	}

	err = c.Send(ctx, wire.TypeSessionInput, wire.SessionInput{
		Stream: started.Stream,
		Data:   base64.StdEncoding.EncodeToString([]byte("hello\n")),
	})
	if err != nil {
		t.Fatalf("session.input: %v", err)
	}
	awaitOutput(ctx, t, c, "hello")

	if err := c.Call(ctx, wire.TypeSessionClose, wire.SessionClose{Stream: started.Stream}, nil); err != nil {
		t.Fatalf("session.close: %v", err)
	}
	awaitExit(ctx, t, c)
}
