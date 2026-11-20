package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"sync"
)

// A minimal Model Context Protocol server speaking JSON-RPC 2.0 over stdio
// (one JSON object per line - the MCP stdio transport). It exposes mexaroot
// as tools an AI agent can call, scoped by an allowlist and an optional
// read-only mode, so an agent can be given phone access without giving it
// everything.

// Protocol version this server speaks. Clients negotiate; any version the
// client proposes is accepted and echoed, because the tool surface here is
// stable across the 2024/2025 spec revisions we care about.
const protocolVersion = "2024-11-05"

// Request is one line of JSON-RPC from the client.
type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  struct {
		Name      string          `json:"name,omitempty"`
		Arguments json.RawMessage `json:"arguments,omitempty"`
	} `json:"params,omitempty"`
}

// Response is one line of JSON-RPC back.
type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  any             `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError mirrors JSON-RPC error objects.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Tool is one capability exposed to the agent.
type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// Handler executes one tool call and returns the text shown to the agent.
type Handler func(ctx context.Context, args json.RawMessage) (string, error)

// Server wires tools to a connection and enforces the allowlist and the
// read-only flag. Safe here means: listed in the allowlist AND not a
// mutating tool when read-only is on.
type Server struct {
	mu       sync.Mutex
	tools    map[string]Tool
	handlers map[string]Handler
	readOnly bool
	// denied counts calls rejected by the allowlist or read-only mode,
	// surfaced in logs so a misconfiguration is visible.
	denied int
}

func NewServer(readOnly bool) *Server {
	return &Server{
		tools:    make(map[string]Tool),
		handlers: make(map[string]Handler),
		readOnly: readOnly,
	}
}

func (s *Server) Register(tool Tool, h Handler) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[tool.Name] = tool
	s.handlers[tool.Name] = h
}

// Denied returns how many calls the allowlist or read-only mode rejected.
func (s *Server) Denied() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.denied
}

func (s *Server) listTools() []Tool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Tool, 0, len(s.tools))
	for name, t := range s.tools {
		if s.readOnly && isMutating(name) {
			continue // read-only: hidden, not just refused
		}
		out = append(out, t)
	}
	return out
}

func (s *Server) allow(name string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tools[name]; !ok {
		return false
	}
	if s.readOnly && isMutating(name) {
		return false
	}
	return true
}

// isMutating reports whether a tool changes the phone's state. Kept as a
// plain switch on purpose: a new tool must decide, at registration review,
// which side it lands on.
func isMutating(name string) bool {
	switch name {
	case "exec", "push", "shell_input":
		return true
	default:
		return false
	}
}

// Serve runs the JSON-RPC loop over a line-delimited reader/writer until the
// input ends or the context is cancelled. Every line is one request; an
// unparseable line gets a JSON-RPC error so the client can recover.
func (s *Server) Serve(ctx context.Context, in io.Reader, out io.Writer) error {
	reader := bufio.NewReader(in)
	var wmu sync.Mutex
	writeLine := func(resp any) error {
		data, err := json.Marshal(resp)
		if err != nil {
			return err
		}
		wmu.Lock()
		defer wmu.Unlock()
		_, err = out.Write(append(data, '\n'))
		return err
	}

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		line, err := reader.ReadString('\n')
		if line != "" {
			if rerr := s.handleLine(strings.TrimSpace(line), writeLine); rerr != nil {
				return rerr
			}
		}
		if err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
	}
}

func (s *Server) handleLine(line string, writeLine func(any) error) error {
	if strings.TrimSpace(line) == "" {
		return nil
	}
	var req Request
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return writeLine(Response{
			JSONRPC: "2.0",
			ID:      json.RawMessage("null"),
			Error:   &RPCError{Code: -32700, Message: "parse error"},
		})
	}

	switch req.Method {
	case "initialize":
		return writeLine(Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"protocolVersion": protocolVersion,
			"capabilities":    map[string]any{"tools": map[string]any{}},
			"serverInfo": map[string]any{
				"name":    "mexaroot",
				"version": versionValue,
			},
		}})
	case "notifications/initialized":
		return nil // a notification: no response
	case "tools/list":
		return writeLine(Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
			"tools": s.listTools(),
		}})
	case "tools/call":
		if !s.allow(req.Params.Name) {
			s.mu.Lock()
			s.denied++
			s.mu.Unlock()
			return writeLine(Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{
				"content": []map[string]string{{"type": "text", "text": fmt.Sprintf("tool %q is not available (allowlist or read-only mode)", req.Params.Name)}},
				"isError": true,
			}})
		}
		text, err := s.handlers[req.Params.Name](context.Background(), req.Params.Arguments)
		result := map[string]any{
			"content": []map[string]string{{"type": "text", "text": text}},
		}
		if err != nil {
			result["isError"] = true
			result["content"] = []map[string]string{{"type": "text", "text": text + err.Error()}}
		}
		return writeLine(Response{JSONRPC: "2.0", ID: req.ID, Result: result})
	case "ping":
		return writeLine(Response{JSONRPC: "2.0", ID: req.ID, Result: map[string]any{}})
	default:
		if req.ID == nil {
			return nil // notification, ignore
		}
		return writeLine(Response{JSONRPC: "2.0", ID: req.ID, Error: &RPCError{Code: -32601, Message: "method not found: " + req.Method}})
	}
}

// versionValue is set by the mexaroot command wiring so the server reports
// the same version the CLI does.
var versionValue = "dev"

// SetVersion lets the command layer inject the real version.
func SetVersion(v string) { versionValue = v }

// TextArgs pulls string arguments out of a tool call's raw arguments.
func TextArgs(args json.RawMessage, keys ...string) (map[string]string, error) {
	var m map[string]any
	out := make(map[string]string, len(keys))
	if len(args) > 0 {
		if err := json.Unmarshal(args, &m); err != nil {
			return nil, fmt.Errorf("decode arguments: %w", err)
		}
	}
	for _, k := range keys {
		switch v := m[k].(type) {
		case string:
			out[k] = v
		case nil:
			out[k] = ""
		case float64:
			out[k] = strings.TrimRight(fmt.Sprintf("%v", v), ".0")
		default:
			out[k] = ""
		}
	}
	return out, nil
}
