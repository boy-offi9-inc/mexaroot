package wire

import (
	"encoding/json"
	"fmt"
)

const Version = 1

type Message struct {
	V       int             `json:"v"`
	ID      string          `json:"id,omitempty"`
	Type    string          `json:"type"`
	OK      *bool           `json:"ok,omitempty"`
	Stream  *uint32         `json:"stream,omitempty"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

func Decode(data []byte) (Message, error) {
	var m Message
	if err := json.Unmarshal(data, &m); err != nil {
		return Message{}, &Error{Code: CodeInvalidRequest, Message: fmt.Sprintf("malformed message: %v", err)}
	}
	if m.Type == "" {
		return Message{}, &Error{Code: CodeInvalidRequest, Message: "missing message type"}
	}
	if m.V <= 0 {
		return Message{}, &Error{Code: CodeInvalidRequest, Message: "missing protocol version"}
	}
	return m, nil
}

func Encode(m Message) ([]byte, error) {
	return json.Marshal(m)
}

func NewRequest(id, msgType string, payload any) (Message, error) {
	raw, err := marshalPayload(payload)
	if err != nil {
		return Message{}, err
	}
	return Message{V: Version, ID: id, Type: msgType, Payload: raw}, nil
}

func NewResponse(req Message, payload any) (Message, error) {
	raw, err := marshalPayload(payload)
	if err != nil {
		return Message{}, err
	}
	ok := true
	return Message{V: Version, ID: req.ID, Type: req.Type, OK: &ok, Payload: raw}, nil
}

func NewErrorResponse(req Message, code, message string) Message {
	ok := false
	return Message{
		V:     Version,
		ID:    req.ID,
		Type:  req.Type,
		OK:    &ok,
		Error: &Error{Code: code, Message: message},
	}
}

func NewEvent(msgType string, stream uint32, payload any) (Message, error) {
	raw, err := marshalPayload(payload)
	if err != nil {
		return Message{}, err
	}
	return Message{V: Version, Type: msgType, Stream: &stream, Payload: raw}, nil
}

func (m Message) DecodePayload(dst any) error {
	raw := m.Payload
	if len(raw) == 0 {
		raw = json.RawMessage(`{}`)
	}
	if err := json.Unmarshal(raw, dst); err != nil {
		return &Error{Code: CodeInvalidRequest, Message: fmt.Sprintf("invalid payload: %v", err)}
	}
	return nil
}

func marshalPayload(payload any) (json.RawMessage, error) {
	if payload == nil {
		return json.RawMessage(`{}`), nil
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal payload: %w", err)
	}
	return raw, nil
}
