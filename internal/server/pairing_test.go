package server

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

// parseOfferURISecret pulls the secret back out of an offer URI the way a
// client would.
func parseOfferURISecret(uri string) (string, error) {
	u, err := url.Parse(uri)
	if err != nil {
		return "", err
	}
	secret := u.Query().Get("secret")
	if secret == "" {
		return "", errors.New("no secret in offer URI")
	}
	return secret, nil
}

func newPairingServer(t *testing.T) (*Server, *httptest.Server) {
	t.Helper()
	store, err := auth.OpenStore(t.TempDir() + "/devices.json")
	if err != nil {
		t.Fatalf("OpenStore: %v", err)
	}
	srv, err := New(Config{Addr: "127.0.0.1:0", Devices: store, Version: "test", Registry: newTestRegistry(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	return srv, ts
}

func postPair(t *testing.T, url string, body pairRequest) *http.Response {
	t.Helper()
	data, _ := json.Marshal(body)
	resp, err := http.Post(url, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("POST %s: %v", url, err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestPairOfferEndToEnd(t *testing.T) {
	srv, ts := newPairingServer(t)
	uri, err := srv.CreatePairOffer("ws://127.0.0.1:8750", "phone", []string{auth.CapExec}, time.Minute)
	if err != nil {
		t.Fatalf("CreatePairOffer: %v", err)
	}
	if !strings.HasPrefix(uri, "mexaroot://pair?") || !strings.Contains(uri, "secret=") {
		t.Fatalf("bad offer URI: %s", uri)
	}

	resp := postPair(t, ts.URL+"/pair", pairRequest{Secret: "wrong", Name: "phone"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("wrong secret: want 401, got %d", resp.StatusCode)
	}

	resp = postPair(t, ts.URL+"/pair", pairRequest{Name: "phone"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("missing secret: want 401, got %d", resp.StatusCode)
	}

	parsed, err := parseOfferURISecret(uri)
	if err != nil {
		t.Fatalf("parse offer: %v", err)
	}
	resp = postPair(t, ts.URL+"/pair", pairRequest{Secret: parsed, Name: "pixel"})
	if resp.StatusCode != http.StatusOK {
		var body bytes.Buffer
		_, _ = body.ReadFrom(resp.Body)
		t.Fatalf("good secret: want 200, got %d: %s", resp.StatusCode, body.String())
	}
	var answer pairResponse
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if answer.Name != "pixel" || answer.Token == "" {
		t.Fatalf("bad answer: %+v", answer)
	}
	if len(answer.Capabilities) != 1 || answer.Capabilities[0] != auth.CapExec {
		t.Fatalf("capabilities not minted as offered: %v", answer.Capabilities)
	}

	// The minted token must actually authenticate a hello.
	ctx, conn := dial(t, "ws"+strings.TrimPrefix(ts.URL, "http")+"/ws")
	helloResp := call(ctx, t, conn, wire.TypeHello, hello(answer.Token, wire.Version))
	if helloResp.OK == nil || !*helloResp.OK {
		t.Fatalf("paired token rejected by handshake: %+v", helloResp.Error)
	}

	// Single use: the second attempt with the same secret burns out.
	resp = postPair(t, ts.URL+"/pair", pairRequest{Secret: parsed, Name: "phone"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused secret: want 401, got %d", resp.StatusCode)
	}
}

func TestPairOfferExpiry(t *testing.T) {
	srv, ts := newPairingServer(t)
	uri, err := srv.CreatePairOffer("ws://127.0.0.1:8750", "phone", nil, time.Nanosecond)
	if err != nil {
		t.Fatalf("CreatePairOffer: %v", err)
	}
	secret, _ := parseOfferURISecret(uri)
	time.Sleep(time.Millisecond)
	resp := postPair(t, ts.URL+"/pair", pairRequest{Secret: secret, Name: "phone"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("expired secret: want 401, got %d", resp.StatusCode)
	}
}

func TestPairOfferRequiresDeviceStore(t *testing.T) {
	srv, err := New(Config{Addr: "127.0.0.1:0", Token: "t", Registry: newTestRegistry(t)})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := srv.CreatePairOffer("ws://127.0.0.1:1", "phone", nil, time.Minute); err == nil {
		t.Fatal("pairing without a device store must be refused")
	}
}

func TestNormalizePairHost(t *testing.T) {
	for _, tc := range []struct{ in, want string }{
		{"127.0.0.1:8750", "127.0.0.1:8750"},
		{":8750", "127.0.0.1:8750"},
		{"0.0.0.0:8750", "127.0.0.1:8750"},
	} {
		if got := NormalizePairHost(tc.in); got != tc.want {
			t.Errorf("NormalizePairHost(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
