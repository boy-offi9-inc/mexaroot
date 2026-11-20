package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"testing"
)

func newTestServer(readOnly bool, register func(*Server)) *Server {
	s := NewServer(readOnly)
	register(s)
	return s
}

func call(t *testing.T, s *Server, lines string) string {
	t.Helper()
	var out bytes.Buffer
	if err := s.Serve(context.Background(), strings.NewReader(lines), &out); err != nil {
		t.Fatalf("serve: %v", err)
	}
	return out.String()
}

func initialize(t *testing.T, s *Server) string {
	t.Helper()
	out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`+"\n")
	var resp struct {
		Result struct {
			ProtocolVersion string `json:"protocolVersion"`
			ServerInfo      struct {
				Name    string `json:"name"`
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &resp); err != nil {
		t.Fatalf("unmarshal: %v (%s)", err, out)
	}
	if resp.Result.ServerInfo.Name != "mexaroot" {
		t.Errorf("serverInfo.name = %q", resp.Result.ServerInfo.Name)
	}
	if resp.Result.ProtocolVersion == "" {
		t.Error("protocolVersion missing")
	}
	return out
}

func TestInitializeRepliesWithServerInfo(t *testing.T) {
	initialize(t, newTestServer(false, func(s *Server) {}))
}

func TestToolsListAndCall(t *testing.T) {
	s := newTestServer(false, func(s *Server) {
		s.Register(Tool{
			Name:        "echo",
			Description: "returns its input",
			InputSchema: map[string]any{"type": "object"},
		}, func(ctx context.Context, args json.RawMessage) (string, error) {
			return "got:" + string(args), nil
		})
	})
	out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"+
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"echo","arguments":{"x":1}}}`+"\n")
	if !strings.Contains(out, `"echo"`) {
		t.Errorf("tools/list missing echo: %s", out)
	}
	if !strings.Contains(out, "got:") {
		t.Errorf("tools/call result missing: %s", out)
	}
}

func TestUnknownToolIsIsErrorNotTransportError(t *testing.T) {
	s := newTestServer(false, func(s *Server) {})
	out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"nope","arguments":{}}}`+"\n")
	if !strings.Contains(out, `"isError":true`) {
		t.Errorf("expected isError result, got: %s", out)
	}
	if s.Denied() != 1 {
		t.Errorf("denied = %d, want 1", s.Denied())
	}
}

func TestReadOnlyHidesMutatingTools(t *testing.T) {
	s := newTestServer(true, func(s *Server) {
		for _, name := range []string{"list_runtimes", "pull", "status", "exec", "push"} {
			s.Register(Tool{Name: name, Description: name, InputSchema: map[string]any{"type": "object"}},
				func(ctx context.Context, args json.RawMessage) (string, error) { return "ran " + name, nil })
		}
	})
	out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`+"\n"+
		`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"exec","arguments":{}}}`+"\n"+
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"pull","arguments":{}}}`+"\n")
	// exec and push are not listed
	if strings.Contains(out, `"name":"exec"`) || strings.Contains(out, `"name":"push"`) {
		t.Errorf("mutating tools leaked into read-only tools/list: %s", out)
	}
	// and not callable
	if !strings.Contains(out, "not available") {
		t.Errorf("exec should be refused in read-only mode: %s", out)
	}
	// pull still works
	if !strings.Contains(out, "ran pull") {
		t.Errorf("pull should work in read-only mode: %s", out)
	}
	if s.Denied() != 1 {
		t.Errorf("denied = %d, want 1 (exec only)", s.Denied())
	}
}

func TestHandlerErrorBecomesIsError(t *testing.T) {
	s := newTestServer(false, func(s *Server) {
		s.Register(Tool{Name: "boom", Description: "", InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, args json.RawMessage) (string, error) {
				return "context: ", context.DeadlineExceeded
			})
	})
	out := call(t, s, `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"boom","arguments":{}}}`+"\n")
	if !strings.Contains(out, `"isError":true`) {
		t.Errorf("expected isError: %s", out)
	}
	if !strings.Contains(out, "context: ") {
		t.Errorf("expected handler text preserved: %s", out)
	}
}

func TestParseErrorRecovers(t *testing.T) {
	s := newTestServer(false, func(s *Server) {
		s.Register(Tool{Name: "ok", Description: "", InputSchema: map[string]any{"type": "object"}},
			func(ctx context.Context, args json.RawMessage) (string, error) { return "fine", nil })
	})
	out := call(t, s, "not json at all\n"+`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"ok","arguments":{}}}`+"\n")
	if !strings.Contains(out, "-32700") {
		t.Errorf("expected parse error code: %s", out)
	}
	if !strings.Contains(out, "fine") {
		t.Errorf("server should keep serving after a bad line: %s", out)
	}
}

func TestUnknownMethodIs32601(t *testing.T) {
	out := call(t, newTestServer(false, func(s *Server) {}),
		`{"jsonrpc":"2.0","id":1,"method":"resources/list"}`+"\n")
	if !strings.Contains(out, "-32601") {
		t.Errorf("expected method-not-found: %s", out)
	}
}

func TestParseAllow(t *testing.T) {
	if got := ParseAllow(""); got != nil {
		t.Errorf("empty -> %#v, want nil", got)
	}
	got := ParseAllow(" list_runtimes, exec , status,")
	want := []string{"list_runtimes", "exec", "status"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("ParseAllow = %#v, want %#v", got, want)
	}
}

func TestEOFEndsServe(t *testing.T) {
	if err := newTestServer(false, func(s *Server) {}).Serve(context.Background(), strings.NewReader(""), &bytes.Buffer{}); err != nil {
		t.Errorf("empty input should end cleanly, got %v", err)
	}
}
