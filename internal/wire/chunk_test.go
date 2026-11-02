package wire

import (
	"errors"
	"testing"
)

func TestChunkRoundTrip(t *testing.T) {
	frame, err := EncodeChunk(7, []byte("data"))
	if err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	stream, data, err := DecodeChunk(frame)
	if err != nil {
		t.Fatalf("DecodeChunk: %v", err)
	}
	if stream != 7 || string(data) != "data" {
		t.Fatalf("unexpected chunk: stream=%d data=%q", stream, data)
	}
}

func TestChunkEndMarker(t *testing.T) {
	frame, err := EncodeChunk(5, nil)
	if err != nil {
		t.Fatalf("EncodeChunk: %v", err)
	}
	stream, data, err := DecodeChunk(frame)
	if err != nil {
		t.Fatalf("DecodeChunk: %v", err)
	}
	if stream != 5 || len(data) != 0 {
		t.Fatalf("unexpected end marker: stream=%d len=%d", stream, len(data))
	}
}

func TestDecodeChunkShortFrame(t *testing.T) {
	_, _, err := DecodeChunk([]byte{1, 2, 3})
	if !errors.Is(err, ErrShortFrame) {
		t.Fatalf("expected ErrShortFrame, got %v", err)
	}
}

func TestEncodeChunkTooLarge(t *testing.T) {
	_, err := EncodeChunk(1, make([]byte, MaxChunkSize+1))
	if !errors.Is(err, ErrChunkTooLarge) {
		t.Fatalf("expected ErrChunkTooLarge, got %v", err)
	}
}
