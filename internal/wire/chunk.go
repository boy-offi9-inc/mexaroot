package wire

import (
	"encoding/binary"
	"errors"
)

const (
	MaxChunkSize    = 65536
	chunkHeaderSize = 4
)

var (
	ErrShortFrame    = errors.New("binary frame is shorter than the stream header")
	ErrChunkTooLarge = errors.New("chunk exceeds the maximum chunk size")
)

func EncodeChunk(stream uint32, data []byte) ([]byte, error) {
	if len(data) > MaxChunkSize {
		return nil, ErrChunkTooLarge
	}
	frame := make([]byte, chunkHeaderSize+len(data))
	binary.BigEndian.PutUint32(frame, stream)
	copy(frame[chunkHeaderSize:], data)
	return frame, nil
}

func DecodeChunk(frame []byte) (uint32, []byte, error) {
	if len(frame) < chunkHeaderSize {
		return 0, nil, ErrShortFrame
	}
	return binary.BigEndian.Uint32(frame), frame[chunkHeaderSize:], nil
}
