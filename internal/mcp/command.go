package mcp

import (
	"flag"
	"strings"
)

// CommandFlags describes the `mexaroot mcp` command's flags so the CLI can
// register them without the mcp package depending on the CLI's option type.
type CommandFlags struct {
	ReadOnly string // -read-only flag value target (bool, via Parse)
	Allow    string // -allow comma-separated tool names
}

// ParseAllow splits a comma-separated allowlist, trimming blanks. Empty
// means "register everything".
func ParseAllow(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	var out []string
	for _, v := range strings.Split(s, ",") {
		if t := strings.TrimSpace(v); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// Usage returns the help text for the mcp command, printed by the CLI.
func Usage() string {
	return `mcp — expose mexaroot as MCP tools over stdio (for AI agents)

Usage: mexaroot mcp [-read-only] [-allow list_runtimes,exec,...]

Runs an MCP (Model Context Protocol) server on stdin/stdout, so an agent
(Claude Desktop, Claude Code, any MCP client) can list runtimes, run
commands, and move files on the phone through the same daemon and token
as the CLI.

Flags:
  -read-only
        expose only read-only tools (list_runtimes, pull, status); exec and
        push are not registered at all
  -allow LIST
        comma-separated allowlist of tools; empty means all

Example Claude Desktop config:
  "mexaroot": {
    "command": "mexaroot",
    "args": ["mcp", "-read-only"],
    "env": { "MEXAROOT_URL": "...", "MEXAROOT_TOKEN": "..." }
  }
`
}

// BindFlags adds the mcp command's flags to a FlagSet and returns the parsed
// options. Kept here so flag registration and help stay in one file.
func BindFlags(fs *flag.FlagSet) (readOnly *bool, allow *string) {
	return fs.Bool("read-only", false, "expose only read-only tools"),
		fs.String("allow", "", "comma-separated allowlist of tools (empty = all)")
}
