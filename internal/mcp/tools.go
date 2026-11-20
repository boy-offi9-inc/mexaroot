package mcp

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

// RegisterTools wires the mexaroot-backed tools onto the server. Every tool
// goes through the same daemon connection and auth as the CLI, so the MCP
// surface adds no new privileges - it can only do what the token allows.
//
// Read-only mode (server-level) hides exec/push/shell_input; the allowlist
// is implemented by simply not registering tools the operator excluded.
func RegisterTools(s *Server, c *client.Client, allowed []string) error {
	var runtimes wire.RuntimeListResponse
	if err := c.Call(context.Background(), wire.TypeRuntimeList, nil, &runtimes); err != nil {
		return fmt.Errorf("list runtimes: %w", err)
	}
	var names []string
	for _, rt := range runtimes.Runtimes {
		names = append(names, rt.ID)
	}
	runtimeDesc := "runtime to run in; one of: " + strings.Join(names, ", ")
	if len(names) == 0 {
		runtimeDesc = "runtime to run in"
	}

	type toolDef struct {
		tool    Tool
		handler Handler
	}
	defs := []toolDef{
		{
			tool: Tool{
				Name:        "list_runtimes",
				Description: "List the runtimes available on the phone (native Termux/host, proot distros, Proroot) and their state.",
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			},
			handler: func(ctx context.Context, _ json.RawMessage) (string, error) {
				var resp wire.RuntimeListResponse
				if err := c.Call(ctx, wire.TypeRuntimeList, nil, &resp); err != nil {
					return "", err
				}
				var b strings.Builder
				for _, rt := range resp.Runtimes {
					fmt.Fprintf(&b, "%s\t%s\t%s\t%s\n", rt.ID, rt.Kind, rt.Name, rt.State)
				}
				return b.String(), nil
			},
		},
		{
			tool: Tool{
				Name:        "exec",
				Description: "Run a command in a runtime on the phone and get stdout/stderr and the exit code. Output is capped at 8 MiB per stream.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"runtime": map[string]any{"type": "string", "description": runtimeDesc},
						"argv":    map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "command and arguments, no shell"},
					},
					"required": []string{"runtime", "argv"},
				},
			},
			handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				var req struct {
					Runtime string   `json:"runtime"`
					Argv    []string `json:"argv"`
				}
				if err := json.Unmarshal(args, &req); err != nil {
					return "", fmt.Errorf("decode arguments: %w", err)
				}
				var resp wire.ExecResponse
				err := c.Call(ctx, wire.TypeExecRun, wire.ExecRequest{
					Runtime: req.Runtime,
					Argv:    req.Argv,
				}, &resp)
				if err != nil {
					return "", err
				}
				var b strings.Builder
				fmt.Fprintf(&b, "exit: %d\n", resp.ExitCode)
				if resp.Truncated {
					b.WriteString("output exceeded the 8 MiB cap and was cut off\n")
				}
				if len(resp.Stdout) > 0 {
					b.WriteString("stdout:\n" + resp.Stdout)
				}
				if len(resp.Stderr) > 0 {
					b.WriteString("stderr:\n" + resp.Stderr)
				}
				return b.String(), nil
			},
		},
		{
			tool: Tool{
				Name:        "push",
				Description: "Push a file from this machine to the phone. Paths are absolute on both sides.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"runtime": map[string]any{"type": "string", "description": runtimeDesc},
						"local":   map[string]any{"type": "string", "description": "path on this machine"},
						"remote":  map[string]any{"type": "string", "description": "destination path on the phone"},
					},
					"required": []string{"runtime", "local", "remote"},
				},
			},
			handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				a, err := TextArgs(args, "runtime", "local", "remote")
				if err != nil {
					return "", err
				}
				if err := c.Push(ctx, a["runtime"], a["remote"], a["local"], 0o644); err != nil {
					return "", err
				}
				return fmt.Sprintf("pushed to %s:%s", a["runtime"], a["remote"]), nil
			},
		},
		{
			tool: Tool{
				Name:        "pull",
				Description: "Pull a file from the phone to this machine. Read-only on the phone. Paths are absolute on both sides.",
				InputSchema: map[string]any{
					"type": "object",
					"properties": map[string]any{
						"runtime": map[string]any{"type": "string", "description": runtimeDesc},
						"remote":  map[string]any{"type": "string", "description": "path on the phone"},
						"local":   map[string]any{"type": "string", "description": "destination path on this machine"},
					},
					"required": []string{"runtime", "remote", "local"},
				},
			},
			handler: func(ctx context.Context, args json.RawMessage) (string, error) {
				a, err := TextArgs(args, "runtime", "remote", "local")
				if err != nil {
					return "", err
				}
				if err := c.Pull(ctx, a["runtime"], a["remote"], a["local"]); err != nil {
					return "", err
				}
				return fmt.Sprintf("pulled %s to %s", a["remote"], a["local"]), nil
			},
		},
		{
			tool: Tool{
				Name:        "status",
				Description: "Connection and daemon status: protocol version, capabilities, daemon version.",
				InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
			},
			handler: func(ctx context.Context, _ json.RawMessage) (string, error) {
				return fmt.Sprintf("daemon: %s\nprotocol: %d\ncapabilities: %s",
					c.Daemon, c.Protocol, strings.Join(c.Capabilities, ", ")), nil
			},
		},
	}

	for _, d := range defs {
		if len(allowed) > 0 && !contains(allowed, d.tool.Name) {
			continue // allowlist: not registered, so not listed and not callable
		}
		s.Register(d.tool, d.handler)
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}
