package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
	"github.com/boy-offi9-inc/mexaroot/internal/keepalive"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/native"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/prootdistro"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/proroot"
	"github.com/boy-offi9-inc/mexaroot/internal/server"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/version"
)

const defaultAddr = "127.0.0.1:8750"

type options struct {
	addr         string
	tokenFile    string
	allowRemote  bool
	forwardAllow string
	noColor      bool
	wakeLock     bool
	auditFile    string
}

func parseOptions(argv []string) (options, error) {
	var opts options
	flag.CommandLine.Init("mexarootd", flag.ContinueOnError)
	flag.CommandLine.SetOutput(os.Stderr)
	flag.StringVar(&opts.addr, "addr", defaultAddr, "address to listen on")
	flag.StringVar(&opts.tokenFile, "token-file", "", "path to the pairing token file")
	flag.BoolVar(&opts.allowRemote, "allow-non-loopback", false, "permit listening on non-loopback addresses")
	flag.StringVar(&opts.forwardAllow, "forward-allow", "", "comma-separated hosts port forwarding may dial, or * for any (default: loopback only)")
	flag.BoolVar(&opts.noColor, "no-color", false, "disable colored output")
	flag.BoolVar(&opts.wakeLock, "wake-lock", true, "hold a Termux wake lock while running")
	flag.StringVar(&opts.auditFile, "audit-log", "", "append a JSON Lines audit log of connections and executed commands to this file (off by default)")
	printToken := flag.Bool("print-token", false, "print the pairing token and exit")
	installService := flag.Bool("install-service", false, "install a termux-services service and exit")
	uninstallService := flag.Bool("uninstall-service", false, "remove the termux-services service and exit")
	showVersion := flag.Bool("version", false, "print the version and exit")
	if err := flag.CommandLine.Parse(argv); err != nil {
		return opts, err
	}

	var err error
	switch {
	case *showVersion:
		fmt.Println(version.VersionString())
	case *installService:
		err = installServiceCommand(opts)
	case *uninstallService:
		err = uninstallServiceCommand(opts)
	case *printToken:
		err = printTokenCommand(opts)
	default:
		err = serve(opts)
	}
	return opts, err
}

func main() {
	_, err := parseOptions(os.Args[1:])
	if err != nil {
		if !errors.Is(err, flag.ErrHelp) {
			log.Fatalf("mexarootd: %v", err)
		}
		os.Exit(2)
	}
}

func splitAllow(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	return parts
}

func loadToken(path string) (string, error) {
	if path == "" {
		defaultPath, err := auth.DefaultPath()
		if err != nil {
			return "", err
		}
		path = defaultPath
	}
	return auth.LoadOrCreate(path)
}

func printTokenCommand(opts options) error {
	token, err := loadToken(opts.tokenFile)
	if err != nil {
		return err
	}
	fmt.Println(token)
	return nil
}

func register(registry *runtimes.Registry, rt runtimes.Runtime) {
	id := rt.Info().ID
	if err := registry.Register(rt); err != nil {
		log.Printf("skipping runtime %s: %v", id, err)
		return
	}
	log.Printf("registered runtime %s", id)
}

func buildRegistry() (*runtimes.Registry, error) {
	registry := runtimes.NewRegistry()
	if err := registry.Register(native.New()); err != nil {
		return nil, err
	}

	distros, err := prootdistro.Discover(prootdistro.DefaultConfig())
	if err != nil {
		log.Printf("proot-distro discovery failed: %v", err)
		return registry, nil
	}
	for _, rt := range distros {
		register(registry, rt)
	}

	prorootCfg := proroot.DefaultConfig()
	if !proroot.Available(prorootCfg) {
		return registry, nil
	}
	for _, distro := range distros {
		rt, err := proroot.New(distro.Info().ID, prorootCfg)
		if err != nil {
			log.Printf("skipping proroot runtime for %s: %v", distro.Info().ID, err)
			continue
		}
		register(registry, rt)
	}
	return registry, nil
}

func acquireWakeLock(styler ui.Styler) func() {
	lock := keepalive.New()
	switch err := lock.Acquire(); {
	case err == nil:
		fmt.Fprintf(os.Stderr, "  %s wake lock held\n", styler.Paint(ui.Green, "●"))
	case errors.Is(err, keepalive.ErrUnavailable):
		fmt.Fprintf(os.Stderr, "  %s %s\n", styler.Paint(ui.Gray, "○"), styler.Dim("wake lock unavailable (termux-wake-lock not found)"))
	default:
		fmt.Fprintf(os.Stderr, "  %s %s\n", styler.Paint(ui.Yellow, "!"), styler.Dim(err.Error()))
	}
	return func() {
		if err := lock.Release(); err != nil && !errors.Is(err, keepalive.ErrUnavailable) {
			log.Printf("release wake lock: %v", err)
		}
	}
}

func serveContext(opts options, ctx context.Context) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	cancelMu.Lock()
	serveCancel = cancel
	cancelMu.Unlock()
	token, err := loadToken(opts.tokenFile)
	if err != nil {
		return err
	}

	styler := ui.New(os.Stderr, opts.noColor)
	fmt.Fprint(os.Stderr, "\n", styler.Banner("mexarootd", version.VersionString(), "Bridging Termux, proot and your PC"))
	fmt.Fprintln(os.Stderr)

	if opts.wakeLock {
		release := acquireWakeLock(styler)
		defer release()
	}

	registry, err := buildRegistry()
	if err != nil {
		return err
	}

	srv, err := server.New(server.Config{
		Addr:             opts.addr,
		Token:            token,
		Version:          version.VersionString(),
		Registry:         registry,
		AllowNonLoopback: opts.allowRemote,
		AuditPath:        opts.auditFile,
		ForwardAllow:     splitAllow(opts.forwardAllow),
	})
	if err != nil {
		return err
	}

	fmt.Fprintf(os.Stderr, "\n  %s listening on %s\n\n", styler.Paint(ui.Green, "●"), styler.Bold(opts.addr))
	if opts.auditFile != "" {
		fmt.Fprintf(os.Stderr, "  %s audit log at %s\n", styler.Paint(ui.Green, "●"), opts.auditFile)
	}
	return srv.ListenAndServe(ctx)
}

func serve(opts options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	return serveContext(opts, ctx)
}

// stopServe cancels the daemon's context; the Windows service handler calls
// it from Execute when the service control manager requests Stop, since
// signal-based shutdown does not reach a service process.
func stopServe() error {
	cancelMu.Lock()
	cancel := serveCancel
	cancelMu.Unlock()
	if cancel == nil {
		return nil
	}
	cancel()
	return nil
}

var (
	cancelMu    sync.Mutex
	serveCancel context.CancelFunc
)
