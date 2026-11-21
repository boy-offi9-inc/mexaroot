package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/version"
)

type commandInfo struct {
	name    string
	summary string
}

var commands = []commandInfo{
	{"runtimes", "List the runtimes known to the daemon"},
	{"exec", "Run a command in a runtime and print its output"},
	{"push", "Copy a local file into a runtime"},
	{"pull", "Copy a file from a runtime to this machine"},
	{"shell", "Open an interactive shell, choosing a runtime if none is given"},
	{"attach", "Re-attach to a resumable session after a disconnect"},
	{"sessions", "List resumable sessions on the daemon"},
	{"forward", "Forward a local TCP port to a host reachable from the daemon"},
	{"status", "Show the daemon version, capabilities, latency, and runtimes"},
	{"mcp", "Expose mexaroot as MCP tools over stdio, for AI agents"},
	{"doctor", "Check the connection step by step and suggest fixes"},
	{"completion", "Print a shell completion script for bash, zsh, or fish"},
	{"help", "Show this help"},
}

var examples = []string{
	"mexaroot runtimes",
	"mexaroot exec ubuntu -- uname -a",
	"mexaroot push ubuntu report.pdf /root/report.pdf",
	"mexaroot pull ubuntu /root/report.pdf report.pdf",
	"mexaroot shell",
	"mexaroot sessions",
	"mexaroot attach 3",
	"mexaroot forward 8080 127.0.0.1 3000",
	"mexaroot status",
	"mexaroot mcp",
	"mexaroot doctor",
	"mexaroot -json runtimes",
	"source <(mexaroot completion bash)",
}

func isCommand(name string) bool {
	for _, command := range commands {
		if command.name == name {
			return true
		}
	}
	return false
}

func commandNames() []string {
	names := make([]string, 0, len(commands))
	for _, command := range commands {
		names = append(names, command.name)
	}
	return names
}

func printHelp(w io.Writer, s ui.Styler, fs *flag.FlagSet) {
	fmt.Fprintln(w)
	fmt.Fprint(w, s.Banner("mexaroot", version.VersionString(), "Termux, proot and your PC, bridged"))
	fmt.Fprintln(w)

	fmt.Fprintln(w, "  "+s.Section("Usage"))
	fmt.Fprintf(w, "    %s %s\n\n", s.Bold("mexaroot"), s.Dim("[flags] <command> [arguments]"))

	fmt.Fprintln(w, "  "+s.Section("Commands"))
	commandRows := make([][]ui.Cell, 0, len(commands))
	for _, command := range commands {
		commandRows = append(commandRows, []ui.Cell{
			{Text: command.name, Color: ui.Cyan, Bold: true},
			{Text: command.summary},
		})
	}
	s.Table(w, "    ", nil, commandRows)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "  "+s.Section("Flags"))
	var flagRows [][]ui.Cell
	fs.VisitAll(func(f *flag.Flag) {
		note := ""
		if f.DefValue != "" && f.DefValue != "false" {
			note = "default " + f.DefValue
		}
		flagRows = append(flagRows, []ui.Cell{
			{Text: "-" + f.Name, Color: ui.Yellow},
			{Text: f.Usage},
			{Text: note, Dim: true},
		})
	})
	s.Table(w, "    ", nil, flagRows)
	fmt.Fprintln(w)

	fmt.Fprintln(w, "  "+s.Section("Examples"))
	for _, example := range examples {
		fmt.Fprintf(w, "    %s %s\n", s.Dim("$"), example)
	}
	fmt.Fprintln(w)
}
