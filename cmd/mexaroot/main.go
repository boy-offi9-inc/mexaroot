package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/version"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const defaultURL = "ws://127.0.0.1:8750/ws"

type options struct {
	url         string
	token       string
	tokenFile   string
	json        bool
	noColor     bool
	showVersion bool
}

func newFlagSet(opts *options) *flag.FlagSet {
	fs := flag.NewFlagSet("mexaroot", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.StringVar(&opts.url, "url", defaultURL, "daemon WebSocket URL")
	fs.StringVar(&opts.token, "token", "", "pairing token (default: MEXAROOT_TOKEN)")
	fs.StringVar(&opts.tokenFile, "token-file", "", "file containing the pairing token")
	fs.BoolVar(&opts.json, "json", false, "print machine-readable JSON where supported")
	fs.BoolVar(&opts.noColor, "no-color", false, "disable colored output")
	fs.BoolVar(&opts.showVersion, "version", false, "print the version and exit")
	return fs
}

func main() {
	os.Exit(execute(os.Args[1:]))
}

func execute(args []string) int {
	var opts options
	fs := newFlagSet(&opts)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			printHelp(os.Stdout, ui.New(os.Stdout, opts.noColor), fs)
			return 0
		}
		printError(os.Stderr, ui.New(os.Stderr, opts.noColor), err)
		return 2
	}

	out := ui.New(os.Stdout, opts.noColor)
	errOut := ui.New(os.Stderr, opts.noColor)

	if opts.showVersion {
		fmt.Println(version.Version)
		return 0
	}

	rest := fs.Args()
	if len(rest) == 0 {
		printHelp(os.Stderr, errOut, fs)
		return 2
	}
	if rest[0] == "help" {
		printHelp(os.Stdout, out, fs)
		return 0
	}

	code, err := run(opts, fs, rest, out, errOut)
	if err != nil {
		if opts.json {
			printJSONError(os.Stderr, err)
		} else {
			printError(os.Stderr, errOut, err)
		}
	}
	return code
}

func run(opts options, fs *flag.FlagSet, args []string, out, errOut ui.Styler) (int, error) {
	name := args[0]
	if !isCommand(name) {
		return 2, fmt.Errorf("unknown command %q (run mexaroot help)", name)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	switch name {
	case "completion":
		return completionCommand(args[1:], fs)
	case "doctor":
		return doctorCommand(ctx, opts, out)
	}

	secret, err := resolveToken(opts.token, opts.tokenFile)
	if err != nil {
		return 1, err
	}

	c, err := client.Connect(ctx, opts.url, secret, "mexaroot/"+version.Version)
	if err != nil {
		return 1, err
	}
	defer c.Close()

	switch name {
	case "runtimes":
		return listRuntimes(ctx, c, opts, out)
	case "exec":
		return execCommand(ctx, c, opts, args[1:])
	case "push":
		return pushCommand(ctx, c, args[1:])
	case "pull":
		return pullCommand(ctx, c, args[1:])
	case "shell":
		return shellCommand(ctx, c, args[1:], errOut)
	case "status":
		return statusCommand(ctx, c, opts, out)
	default:
		return 2, fmt.Errorf("unknown command %q (run mexaroot help)", name)
	}
}

func tokenSource(token, tokenFile string) (string, string, error) {
	if token != "" {
		return token, "the -token flag", nil
	}
	if env := os.Getenv("MEXAROOT_TOKEN"); env != "" {
		return env, "MEXAROOT_TOKEN", nil
	}
	if tokenFile == "" {
		path, err := auth.DefaultPath()
		if err != nil {
			return "", "", err
		}
		tokenFile = path
	}
	secret, err := auth.Load(tokenFile)
	if err != nil {
		return "", "", fmt.Errorf("read token: %w (use -token, -token-file, or MEXAROOT_TOKEN)", err)
	}
	return secret, tokenFile, nil
}

func resolveToken(token, tokenFile string) (string, error) {
	secret, _, err := tokenSource(token, tokenFile)
	return secret, err
}

func listRuntimes(ctx context.Context, c *client.Client, opts options, s ui.Styler) (int, error) {
	var resp wire.RuntimeListResponse
	if err := c.Call(ctx, wire.TypeRuntimeList, nil, &resp); err != nil {
		return 1, err
	}

	if opts.json {
		return 0, writeJSON(os.Stdout, resp)
	}

	if !ui.IsTerminal(os.Stdout) {
		for _, rt := range resp.Runtimes {
			fmt.Printf("%s\t%s\t%s\t%s\n", rt.ID, rt.Kind, rt.State, rt.Name)
		}
		return 0, nil
	}

	if len(resp.Runtimes) == 0 {
		fmt.Println(s.Dim("No runtimes registered."))
		return 0, nil
	}
	rows := make([][]ui.Cell, 0, len(resp.Runtimes))
	for _, rt := range resp.Runtimes {
		rows = append(rows, []ui.Cell{
			{Text: rt.ID, Color: ui.Cyan, Bold: true},
			{Text: rt.Kind},
			ui.StateCell(rt.State),
			{Text: rt.Name, Dim: true},
		})
	}
	fmt.Println()
	s.Table(os.Stdout, "  ", []string{"id", "kind", "state", "name"}, rows)
	fmt.Println()
	return 0, nil
}

func execCommand(ctx context.Context, c *client.Client, opts options, args []string) (int, error) {
	errUsage := errors.New("usage: mexaroot exec <runtime> [--] <command> [args...]")
	if len(args) < 2 {
		return 2, errUsage
	}
	target := args[0]
	argv := args[1:]
	if argv[0] == "--" {
		argv = argv[1:]
	}
	if len(argv) == 0 {
		return 2, errUsage
	}

	var resp wire.ExecResponse
	err := c.Call(ctx, wire.TypeExecRun, wire.ExecRequest{Runtime: target, Argv: argv}, &resp)
	if err != nil {
		return 1, err
	}

	if opts.json {
		if err := writeJSON(os.Stdout, resp); err != nil {
			return 1, err
		}
		return resp.ExitCode, nil
	}
	fmt.Fprint(os.Stdout, resp.Stdout)
	fmt.Fprint(os.Stderr, resp.Stderr)
	if resp.Truncated {
		fmt.Fprintln(os.Stderr, "mexaroot: output exceeded the 8 MiB exec cap and was cut off")
	}
	return resp.ExitCode, nil
}
