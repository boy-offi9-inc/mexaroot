package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/signal"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
	"github.com/boy-offi9-inc/mexaroot/internal/client"
	"github.com/boy-offi9-inc/mexaroot/internal/version"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const defaultURL = "ws://127.0.0.1:8750/ws"

const usageText = `Usage: mexaroot [flags] <command>

Commands:
  runtimes                              list the runtimes known to the daemon
  exec <runtime> [--] <command> [args]  run a command and print its output
  shell <runtime> [shell]               open an interactive shell

Flags:
`

func main() {
	url := flag.String("url", defaultURL, "daemon WebSocket URL")
	token := flag.String("token", "", "pairing token (default: MEXAROOT_TOKEN)")
	tokenFile := flag.String("token-file", "", "path to a file containing the pairing token")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usageText)
		flag.PrintDefaults()
	}
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		os.Exit(2)
	}

	code, err := run(*url, *token, *tokenFile, args)
	if err != nil {
		fmt.Fprintf(os.Stderr, "mexaroot: %v\n", err)
	}
	os.Exit(code)
}

func run(url, token, tokenFile string, args []string) (int, error) {
	secret, err := resolveToken(token, tokenFile)
	if err != nil {
		return 1, err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	c, err := client.Connect(ctx, url, secret, "mexaroot/"+version.Version)
	if err != nil {
		return 1, err
	}
	defer c.Close()

	switch args[0] {
	case "runtimes":
		return listRuntimes(ctx, c)
	case "exec":
		return execCommand(ctx, c, args[1:])
	case "shell":
		return shellCommand(ctx, c, args[1:])
	default:
		return 2, fmt.Errorf("unknown command %q", args[0])
	}
}

func resolveToken(token, tokenFile string) (string, error) {
	if token != "" {
		return token, nil
	}
	if env := os.Getenv("MEXAROOT_TOKEN"); env != "" {
		return env, nil
	}
	if tokenFile == "" {
		path, err := auth.DefaultPath()
		if err != nil {
			return "", err
		}
		tokenFile = path
	}
	secret, err := auth.Load(tokenFile)
	if err != nil {
		return "", fmt.Errorf("read token: %w (use -token, -token-file, or MEXAROOT_TOKEN)", err)
	}
	return secret, nil
}

func listRuntimes(ctx context.Context, c *client.Client) (int, error) {
	var resp wire.RuntimeListResponse
	if err := c.Call(ctx, wire.TypeRuntimeList, nil, &resp); err != nil {
		return 1, err
	}
	for _, rt := range resp.Runtimes {
		fmt.Printf("%s\t%s\t%s\t%s\n", rt.ID, rt.Kind, rt.State, rt.Name)
	}
	return 0, nil
}

func execCommand(ctx context.Context, c *client.Client, args []string) (int, error) {
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
	fmt.Fprint(os.Stdout, resp.Stdout)
	fmt.Fprint(os.Stderr, resp.Stderr)
	return resp.ExitCode, nil
}
