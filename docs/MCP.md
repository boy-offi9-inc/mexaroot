# mexaroot as an MCP server

`mexaroot mcp` runs a [Model Context Protocol](https://modelcontextprotocol.io)
server on stdin/stdout, so an AI agent - Claude Desktop, Claude Code, or any
MCP client - can work with the phone's runtimes: list them, run commands,
move files, read status. It connects to the daemon exactly like the CLI,
with the same token, so it grants no privilege the token doesn't already
have.

## Tools

| Tool | What it does | Read-only? |
|---|---|---|
| `list_runtimes` | list runtimes and their state | yes |
| `exec` | run a command (argv, no shell) in a runtime; stdout/stderr + exit code; 8 MiB per-stream cap | no |
| `push` | copy a file from this machine into a runtime | no |
| `pull` | copy a file from a runtime to this machine | yes |
| `status` | daemon version, protocol, capabilities | yes |

## Scoping what an agent can do

Two mechanisms, both enforced before a tool is even visible:

- **`-read-only`** — the mutating tools (`exec`, `push`) are not registered
  at all: they don't appear in `tools/list`, and a call to them returns a
  tool-level error. Use this for agents that should look but not touch.
- **`-allow list_runtimes,exec,...`** — a comma-separated allowlist; only
  the named tools are registered. Empty means all. Combine with
  `-read-only` if you like; read-only always wins.

Refused or unknown tools come back as `isError` tool results (the agent
sees the reason), never as a crash or a transport failure.

## Setup example (Claude Desktop)

Run the daemon on the phone as usual, then:

```json
{
  "mcpServers": {
    "mexaroot": {
      "command": "mexaroot",
      "args": ["mcp", "-read-only"],
      "env": {
        "MEXAROOT_URL": "ws://127.0.0.1:8750/ws",
        "MEXAROOT_TOKEN": "<the pairing token>"
      }
    }
  }
}
```

Omit `-read-only` (or pass an explicit `-allow` list) when the agent should
be able to run commands. The daemon URL is the same one `mexaroot exec`
uses; if the phone is remote, use the same SSH tunnel or `-allow-non-loopback`
setup as the CLI.

## Security notes

- The MCP server inherits the daemon's protections: handshake rate limiting,
  constant-time token comparison, and (when `-audit-log` is on) every
  connection and executed command is recorded — including the ones an agent
  makes. Point the audit log at a file when an agent has exec access; it is
  the honest record of what the agent actually did.
- There is no interactive-session tool yet (`shell`); agents get bounded
  `exec` calls only.
- Argv is passed without a shell, so shell-injection via the agent's
  arguments is not a thing; the usual caution about *what* you let an agent
  run still applies — `-read-only` exists for a reason.
