package wire

import "testing"

func TestExecRequestRoundTrip(t *testing.T) {
	req, err := NewRequest("1", TypeExecRun, ExecRequest{
		Runtime:   "termux",
		Argv:      []string{"uname", "-a"},
		TimeoutMS: 30000,
	})
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
	var payload ExecRequest
	if err := got.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if payload.Runtime != "termux" || len(payload.Argv) != 2 || payload.TimeoutMS != 30000 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}

func TestHelloResponseRoundTrip(t *testing.T) {
	req := Message{V: Version, ID: "1", Type: TypeHello}
	resp, err := NewResponse(req, HelloResponse{
		Daemon:       "dev",
		Protocol:     Version,
		Capabilities: []string{CapabilityExec, CapabilityFile},
	})
	if err != nil {
		t.Fatalf("NewResponse: %v", err)
	}
	var payload HelloResponse
	if err := resp.DecodePayload(&payload); err != nil {
		t.Fatalf("DecodePayload: %v", err)
	}
	if payload.Protocol != Version || len(payload.Capabilities) != 2 {
		t.Fatalf("unexpected payload: %+v", payload)
	}
}
