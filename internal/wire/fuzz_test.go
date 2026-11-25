package wire

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

// Fuzzing the wire decoder: hostile bytes must never panic the parser - the
// decoder sits on the network path, so every crash class here is remotely
// reachable. Run locally with:
//   go test ./internal/wire -run FuzzDecode -fuzz FuzzDecodeMessage -fuzztime 60s
// CI runs the seed corpus only (go test includes corpus entries as unit tests).

func FuzzDecodeMessage(f *testing.F) {
	// seed with the shapes the protocol actually carries
	valid, err := json.Marshal(Message{V: Version, ID: "1", Type: "exec.run", Payload: json.RawMessage(`{"cmd":"ls"}`)})
	if err != nil {
		f.Fatal(err)
	}
	f.Add(valid)
	f.Add([]byte(`{"v":1,"type":"x"}`))
	f.Add([]byte(`{"v":0,"type":"x"}`))
	f.Add([]byte(`{"v":1}`))
	f.Add([]byte(`{"v":1,"type":""}`))
	f.Add([]byte(`{`))
	f.Add([]byte(``))
	f.Add([]byte{0x00, 0x01, 0xff})
	f.Add([]byte(`{"v":1,"type":"a","payload":` + strings.Repeat("[", 10000) + `}`))
	f.Add([]byte(`{"v":1,"type":"a","payload":"` + strings.Repeat("\xf0\x9f\x92\xa9", 1000) + `"}`))

	f.Fuzz(func(t *testing.T, data []byte) {
		// the contract: never panic; on failure return a *Error, not a bare
		// error; on success the message re-encodes and round-trips with the
		// version, type and raw payload bytes preserved
		m, err := Decode(data)
		if err != nil {
			var wireErr *Error
			if !errors.As(err, &wireErr) {
				t.Fatalf("non-wire error for %q: %v", data, err)
			}
			return
		}
		if m.V <= 0 || m.Type == "" {
			t.Fatalf("decoded invalid message from %q: %+v", data, m)
		}
		encoded, err := Encode(m)
		if err != nil {
			t.Fatalf("re-encode failed: %v", err)
		}
		m2, err := Decode(encoded)
		if err != nil {
			t.Fatalf("round-trip decode failed: %v", err)
		}
		if m2.V != m.V || m2.Type != m.Type {
			t.Fatalf("round-trip mismatch: %+v vs %+v", m, m2)
		}
		// payloads compare semantically, not byte-for-byte: json.Marshal
		// HTML-escapes (& -> \\u0026), so raw bytes can differ while the JSON
		// is equivalent
		var p1, p2 any
		if len(m.Payload) > 0 {
			if err := json.Unmarshal(m.Payload, &p1); err != nil {
				t.Fatalf("payload not valid json: %v", err)
			}
			if err := json.Unmarshal(m2.Payload, &p2); err != nil {
				t.Fatalf("re-encoded payload not valid json: %v", err)
			}
			if !reflect.DeepEqual(p1, p2) {
				t.Fatalf("payload round-trip mismatch: %s vs %s", m.Payload, m2.Payload)
			}
		}
	})
}
