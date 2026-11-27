package wire

import (
	"encoding/json"
	"testing"
)

// Conformance suite: the protocol contract every peer implementation must
// satisfy, independent of how the daemon or client are built. If a change
// breaks one of these, older and newer binaries stop interoperating.

func TestConformanceMessageRoundTrip(t *testing.T) {
	stream := uint32(7)
	ok := true
	msgs := []Message{
		{V: Version, ID: "r1", Type: TypeHello, Payload: json.RawMessage(`{"client":"mexaroot-test","proto":1}`)},
		{V: Version, ID: "r2", Type: TypeExecRun, OK: &ok, Payload: json.RawMessage(`{"runtime":"termux","argv":["echo","hi"]}`)},
		{V: Version, Type: TypeSessionOutput, Stream: &stream, Payload: json.RawMessage(`{"data":"aGk="}`)},
		{V: Version, ID: "r3", Type: TypeExecRun, OK: new(bool), Error: &Error{Code: CodeInvalidRequest, Message: "no such runtime"}},
		{V: Version, Type: TypePing},
	}
	for _, m := range msgs {
		encoded, err := Encode(m)
		if err != nil {
			t.Fatalf("encode %s: %v", m.Type, err)
		}
		decoded, err := Decode(encoded)
		if err != nil {
			t.Fatalf("decode %s: %v", m.Type, err)
		}
		if decoded.V != m.V || decoded.ID != m.ID || decoded.Type != m.Type {
			t.Fatalf("round trip changed envelope: got %+v want %+v", decoded, m)
		}
		if (decoded.OK == nil) != (m.OK == nil) || (decoded.OK != nil && *decoded.OK != *m.OK) {
			t.Fatalf("round trip changed ok: got %+v want %+v", decoded.OK, m.OK)
		}
		if (decoded.Stream == nil) != (m.Stream == nil) || (decoded.Stream != nil && *decoded.Stream != *m.Stream) {
			t.Fatalf("round trip changed stream: got %+v want %+v", decoded.Stream, m.Stream)
		}
		if decoded.Error == nil != (m.Error == nil) || (decoded.Error != nil && *decoded.Error != *m.Error) {
			t.Fatalf("round trip changed error: got %+v want %+v", decoded.Error, m.Error)
		}
		if string(decoded.Payload) != string(m.Payload) {
			t.Fatalf("round trip changed payload: got %s want %s", decoded.Payload, m.Payload)
		}
	}
}

func TestConformanceDecodeRejections(t *testing.T) {
	cases := []struct {
		name string
		data string
	}{
		{"malformed json", `{not json`},
		{"missing type", `{"v":1}`},
		{"missing version", `{"type":"ping"}`},
		{"zero version", `{"v":0,"type":"ping"}`},
		{"negative version", `{"v":-1,"type":"ping"}`},
		{"empty object", `{}`},
		{"not an object", `["ping"]`},
	}
	for _, tc := range cases {
		m, err := Decode([]byte(tc.data))
		if err == nil {
			t.Fatalf("%s: expected rejection, got %+v", tc.name, m)
		}
		werr, ok := err.(*Error)
		if !ok {
			t.Fatalf("%s: error must be *wire.Error, got %T", tc.name, err)
		}
		if werr.Code != CodeInvalidRequest {
			t.Fatalf("%s: expected CodeInvalidRequest, got %s", tc.name, werr.Code)
		}
	}
}

func TestConformanceFutureTypeStillDecodes(t *testing.T) {
	// A newer peer may speak a type an older peer has never heard of: the
	// envelope must still decode so the peer can answer a clean error
	// instead of dropping the connection.
	m, err := Decode([]byte(`{"v":1,"id":"r9","type":"time.travel","payload":{"year":3000}}`))
	if err != nil {
		t.Fatalf("unknown type must decode: %v", err)
	}
	if m.Type != "time.travel" || m.ID != "r9" {
		t.Fatalf("envelope fields lost: %+v", m)
	}
	if cap, known := RequiredCapability("time.travel"); known {
		t.Fatalf("unknown type must not claim a capability, got %q", cap)
	}
}

func TestConformanceKnownTypesHaveCapabilities(t *testing.T) {
	// Every declared message type must be routable to its capability so the
	// daemon's permission checks cannot silently miss a new type.
	core := map[string]bool{TypeHello: true, TypeRuntimeList: true}
	for _, msgType := range []string{
		TypeHello, TypeRuntimeList, TypeExecRun,
		TypeSessionStart, TypeSessionList, TypeSessionAttach, TypeSessionInput, TypeSessionResize, TypeSessionClose,
		TypeFilePush, TypeFilePull,
		TypeForwardOpen, TypeForwardData, TypeForwardClose,
		TypePing,
	} {
		cap, known := RequiredCapability(msgType)
		if !known {
			t.Fatalf("%s: declared type has no capability mapping", msgType)
		}
		if !core[msgType] && cap == "" {
			t.Fatalf("%s: non-core type mapped to empty capability", msgType)
		}
	}
}

func TestConformanceNegotiation(t *testing.T) {
	cases := []struct {
		name   string
		client []int
		server []int
		want   int
		wantOK bool
	}{
		{"exact match", []int{1}, []int{1}, 1, true},
		{"highest common wins", []int{1, 3}, []int{2, 3, 1}, 3, true},
		{"server newer accepts older client", []int{1}, []int{1, 2, 3}, 1, true},
		{"client newer picks shared", []int{1, 2, 3}, []int{1}, 1, true},
		{"no overlap", []int{2, 3}, []int{1}, 0, false},
		{"empty client", nil, []int{1}, 0, false},
		{"empty server", []int{1}, nil, 0, false},
		{"unsorted lists", []int{3, 1}, []int{1, 3}, 3, true},
		{"duplicate entries", []int{1, 1}, []int{1, 1}, 1, true},
	}
	for _, tc := range cases {
		got, ok := Negotiate(tc.client, tc.server)
		if ok != tc.wantOK || got != tc.want {
			t.Fatalf("%s: Negotiate(%v, %v) = (%d, %v), want (%d, %v)", tc.name, tc.client, tc.server, got, ok, tc.want, tc.wantOK)
		}
	}
	// Only version 1 exists today; a lone list must agree on it.
	got, ok := Negotiate(SupportedVersions, SupportedVersions)
	if !ok || got != Version {
		t.Fatalf("SupportedVersions must self-negotiate to Version, got (%d, %v)", got, ok)
	}
}

func TestConformanceChunkFraming(t *testing.T) {
	stream := uint32(0xFFFFFFFF)
	data := []byte("stream-chunk")
	encoded, err := EncodeChunk(stream, data)
	if err != nil {
		t.Fatalf("encode: %v", err)
	}
	gotStream, gotData, err := DecodeChunk(encoded)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if gotStream != stream || string(gotData) != string(data) {
		t.Fatalf("round trip changed chunk: stream=%d data=%q", gotStream, gotData)
	}

	// Empty payload is a legal chunk (end-of-stream marker style).
	encoded, err = EncodeChunk(1, nil)
	if err != nil {
		t.Fatalf("encode empty: %v", err)
	}
	s, d, err := DecodeChunk(encoded)
	if err != nil || s != 1 || len(d) != 0 {
		t.Fatalf("empty chunk round trip: s=%d len=%d err=%v", s, len(d), err)
	}

	// Rejections: short frames and oversized chunks.
	if _, _, err := DecodeChunk([]byte{0x00}); err != ErrShortFrame {
		t.Fatalf("short frame: got %v, want ErrShortFrame", err)
	}
	if _, err := EncodeChunk(1, make([]byte, MaxChunkSize+1)); err != ErrChunkTooLarge {
		t.Fatalf("oversized chunk: got %v, want ErrChunkTooLarge", err)
	}
	// Exactly at the cap must be legal.
	if _, err := EncodeChunk(1, make([]byte, MaxChunkSize)); err != nil {
		t.Fatalf("chunk at cap: %v", err)
	}
}

func TestConformanceErrorResponses(t *testing.T) {
	req, err := NewRequest("req-1", TypeExecRun, ExecRequest{Runtime: "termux", Argv: []string{"ls"}})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	resp, err := NewResponse(req, ExecResponse{})
	if err != nil {
		t.Fatalf("response: %v", err)
	}
	if resp.ID != req.ID || resp.Type != req.Type || resp.OK == nil || !*resp.OK {
		t.Fatalf("response must echo id/type and set ok: %+v", resp)
	}
	errResp := NewErrorResponse(req, CodeInternal, "boom")
	if errResp.OK == nil || *errResp.OK {
		t.Fatalf("error response must carry ok=false: %+v", errResp)
	}
	if errResp.Error == nil || errResp.Error.Code != CodeInternal || errResp.Error.Message != "boom" {
		t.Fatalf("error response lost its error: %+v", errResp)
	}
	if errResp.ID != req.ID || errResp.Type != req.Type {
		t.Fatalf("error response must echo id/type: %+v", errResp)
	}

	// An event is broadcast: no id, no ok, but a stream number.
	event, err := NewEvent(TypeSessionExit, 3, SessionInfo{})
	if err != nil {
		t.Fatalf("event: %v", err)
	}
	if event.ID != "" || event.OK != nil || event.Stream == nil || *event.Stream != 3 {
		t.Fatalf("event envelope wrong: %+v", event)
	}
}

func TestConformanceEmptyPayloadDecodesToStruct(t *testing.T) {
	// A message with no payload decodes into the zero struct, so peers may
	// omit the field for payload-free types.
	m, err := Decode([]byte(`{"v":1,"type":"ping"}`))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	var hello HelloRequest
	if err := m.DecodePayload(&hello); err != nil {
		t.Fatalf("empty payload must decode to zero struct: %v", err)
	}
}
