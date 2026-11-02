package wire

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestDecodeValid(t *testing.T) {
	m, err := Decode([]byte(`{"v":1,"id":"1","type":"ping","payload":{}}`))
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if m.V != 1 || m.ID != "1" || m.Type != "ping" {
		t.Fatalf("unexpected message: %+v", m)
	}
}

func TestDecodeRejectsInvalid(t *testing.T) {
	cases := map[string]string{
		"malformed JSON":  `{`,
		"missing type":    `{"v":1,"id":"1"}`,
		"missing version": `{"id":"1","type":"ping"}`,
	}
	for name, input := range cases {
		t.Run(name, func(t *testing.T) {
			_, err := Decode([]byte(input))
			var werr *Error
			if !errors.As(err, &werr) || werr.Code != CodeInvalidRequest {
				t.Fatalf("expected invalid_request error, got %v", err)
			}
		})
	}
}

func TestNewResponseEchoesRequest(t *testing.T) {
	req := Message{V: Version, ID: "42", Type: "runtime.list"}
	resp, err := NewResponse(req, map[string]any{"runtimes": []string{}})
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	if resp.ID != "42" || resp.Type != "runtime.list" {
		t.Fatalf("response does not echo request: %+v", resp)
	}
	if resp.OK == nil || !*resp.OK {
		t.Fatal("expected ok response")
	}
}

func TestNewErrorResponse(t *testing.T) {
	req := Message{V: Version, ID: "7", Type: "exec.run"}
	resp := NewErrorResponse(req, CodeNotFound, "unknown runtime")
	if resp.OK == nil || *resp.OK {
		t.Fatal("expected failed response")
	}
	if resp.Error == nil || resp.Error.Code != CodeNotFound {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
}

func TestNewEventCarriesStream(t *testing.T) {
	ev, err := NewEvent("session.exit", 9, map[string]int{"exit_code": 0})
	if err != nil {
		t.Fatalf("NewEvent: %v", err)
	}
	if ev.ID != "" || ev.Stream == nil || *ev.Stream != 9 {
		t.Fatalf("unexpected event: %+v", ev)
	}
}

func TestEncodeDecodeRoundTrip(t *testing.T) {
	req, err := NewRequest("3", "ping", nil)
	if err != nil {
		t.Fatalf("NewRequest: %v", err)
	}
	data, err := Encode(req)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	got, err := Decode(data)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got.ID != "3" || got.Type != "ping" || got.V != Version {
		t.Fatalf("unexpected message: %+v", got)
	}
}

func TestDecodePayloadAcceptsEmpty(t *testing.T) {
	var payload struct{}
	if err := (Message{}).DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
}

func TestDecodePayloadRejectsMalformed(t *testing.T) {
	m := Message{Payload: json.RawMessage(`[1]`)}
	var payload struct{ Name string }
	err := m.DecodePayload(&payload)
	var werr *Error
	if !errors.As(err, &werr) || werr.Code != CodeInvalidRequest {
		t.Fatalf("expected invalid_request error, got %v", err)
	}
}
