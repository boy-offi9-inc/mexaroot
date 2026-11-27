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
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
	"github.com/boy-offi9-inc/mexaroot/internal/keepalive"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/native"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/prootdistro"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/proroot"
	qrcode "github.com/skip2/go-qrcode"

	"github.com/boy-offi9-inc/mexaroot/internal/server"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/version"
)

const defaultAddr = "127.0.0.1:8750"

type options struct {
	addr         string
	tokenFile    string
	devicesFile  string
	allowRemote  bool
	forwardAllow string
	noColor      bool
	wakeLock     bool
	auditFile    string
	pair         bool
	pairName     string
	pairCaps     string
	pairAll      bool
	pairTTL      time.Duration
}

func parseOptions(argv []string) (options, error) {
	var opts options
	if len(argv) > 0 && argv[0] == "devices" {
		return opts, devicesCommand(argv[1:])
	}
	flag.CommandLine.Init("mexarootd", flag.ContinueOnError)
	flag.CommandLine.SetOutput(os.Stderr)
	flag.StringVar(&opts.addr, "addr", defaultAddr, "address to listen on")
	flag.StringVar(&opts.tokenFile, "token-file", "", "path to the pairing token file")
	flag.StringVar(&opts.devicesFile, "devices", "", "path to the per-device token store (enables per-device tokens; off by default)")
	flag.BoolVar(&opts.allowRemote, "allow-non-loopback", false, "permit listening on non-loopback addresses")
	flag.StringVar(&opts.forwardAllow, "forward-allow", "", "comma-separated hosts port forwarding may dial, or * for any (default: loopback only)")
	flag.BoolVar(&opts.noColor, "no-color", false, "disable colored output")
	flag.BoolVar(&opts.wakeLock, "wake-lock", true, "hold a Termux wake lock while running")
	flag.StringVar(&opts.auditFile, "audit-log", "", "append a JSON Lines audit log of connections and executed commands to this file (off by default)")
	flag.BoolVar(&opts.pair, "pair", false, "print a pairing offer (QR + URI) for a new device and start the daemon")
	flag.StringVar(&opts.pairName, "pair-name", "", "device name suggested in the pairing offer (default: the client's choice)")
	flag.StringVar(&opts.pairCaps, "pair-caps", "", "capabilities to grant the paired device: exec, shell, files.read, files.write, forward, files (default: read-only)")
	flag.BoolVar(&opts.pairAll, "pair-all", false, "grant the paired device every capability")
	flag.DurationVar(&opts.pairTTL, "pair-ttl", server.DefaultPairTTL, "how long the pairing offer stays valid")
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

func devicesPath(path string) (string, error) {
	if path != "" {
		return path, nil
	}
	return auth.DefaultDevicesPath()
}

// devicesCommand manages per-device tokens without starting the daemon:
//
//	mexarootd devices add <name> [-capabilities a,b | -all]
//	mexarootd devices set <name> [-capabilities a,b | -all]
//	mexarootd devices list
//	mexarootd devices revoke <name>
//	mexarootd devices rotate <name>
//
// New devices start read-only (they may list runtimes and sessions but run
// nothing) — the ttyd lesson. Grant capabilities explicitly.
func devicesCommand(args []string) error {
	fs := flag.NewFlagSet("mexarootd devices", flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	path := fs.String("devices", "", "path to the per-device token store (default: next to the shared token)")
	caps := fs.String("capabilities", "", "comma-separated capabilities to grant: exec, shell, files.read, files.write, forward, files")
	all := fs.Bool("all", false, "grant every capability")
	if err := fs.Parse(flagsFirst(args, map[string]bool{"capabilities": true, "devices": true})); err != nil {
		return err
	}
	_ = path
	store, err := devicesPath(*path)
	if err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: mexarootd devices {add|list|revoke|rotate} [name]")
	}
	s, err := auth.OpenStore(store)
	if err != nil {
		return err
	}
	switch fs.Arg(0) {
	case "add", "set":
		cmd := fs.Arg(0)
		if fs.NArg() != 2 {
			return fmt.Errorf("usage: mexarootd devices %s <name> [-capabilities a,b | -all]", cmd)
		}
		caps, err := parseCapabilities(caps, all)
		if err != nil {
			return err
		}
		if cmd == "add" {
			device, err := s.Create(fs.Arg(1), caps)
			if err != nil {
				return err
			}
			fmt.Printf("device %s created with token:\n%s\n", device.Name, device.Token)
			printCaps(device.Capabilities)
			return nil
		}
		device, err := s.SetCapabilities(fs.Arg(1), caps)
		if err != nil {
			return err
		}
		fmt.Printf("device %s capabilities updated\n", device.Name)
		printCaps(device.Capabilities)
	case "list":
		list, err := s.List()
		if err != nil {
			return err
		}
		if len(list) == 0 {
			fmt.Println("no devices")
			return nil
		}
		for _, d := range list {
			rotated := ""
			if d.RotatedAt != nil {
				rotated = fmt.Sprintf(" (rotated %s)", d.RotatedAt.Format("2006-01-02"))
			}
			fmt.Printf("%s  created %s%s  capabilities: %s\n", d.Name, d.CreatedAt.Format("2006-01-02"), rotated, capsLabel(d.Capabilities))
		}
	case "revoke":
		if fs.NArg() != 2 {
			return fmt.Errorf("usage: mexarootd devices revoke <name>")
		}
		if err := s.Revoke(fs.Arg(1)); err != nil {
			return err
		}
		fmt.Printf("device %s revoked\n", fs.Arg(1))
	case "rotate":
		if fs.NArg() != 2 {
			return fmt.Errorf("usage: mexarootd devices rotate <name>")
		}
		device, err := s.Rotate(fs.Arg(1))
		if err != nil {
			return err
		}
		fmt.Printf("device %s rotated, new token:\n%s\n", device.Name, device.Token)
	default:
		return fmt.Errorf("unknown devices command %q (want add, list, revoke or rotate)", fs.Arg(0))
	}
	return nil
}

// flagsFirst hoists flag tokens ahead of positional arguments so
// "devices add phone -capabilities exec" works — Go's flag package would
// otherwise stop parsing at the first positional.
func flagsFirst(args []string, valued map[string]bool) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			name := strings.TrimLeft(args[i], "-")
			if valued[name] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return append(flags, rest...)
}

func parseCapabilities(caps *string, all *bool) ([]string, error) {
	if all != nil && *all {
		return auth.ExpandCapabilities([]string{auth.GroupAll})
	}
	if caps == nil || *caps == "" {
		// Default: read-only. The device can connect, list runtimes and
		// sessions, and do nothing else — grant explicitly.
		return []string{}, nil
	}
	return auth.ExpandCapabilities(strings.Split(*caps, ","))
}

func capsLabel(caps []string) string {
	if caps == nil {
		return "full (legacy)"
	}
	if len(caps) == 0 {
		return "read-only"
	}
	return strings.Join(caps, ",")
}

func printCaps(caps []string) {
	fmt.Printf("capabilities: %s\n", capsLabel(caps))
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
	token, tokenErr := loadToken(opts.tokenFile)
	if opts.pair && opts.devicesFile == "" {
		defaultDevices, err := auth.DefaultDevicesPath()
		if err != nil {
			return err
		}
		opts.devicesFile = defaultDevices
	}
	var devices *auth.Store
	var err error
	if opts.devicesFile != "" {
		devices, err = auth.OpenStore(opts.devicesFile)
		if err != nil {
			return err
		}
		// An empty shared token file is valid in device-only mode.
		if errors.Is(tokenErr, auth.ErrEmptyToken) {
			token = ""
			tokenErr = nil
		}
	}
	if tokenErr != nil {
		return tokenErr
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
		Devices:          devices,
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
	if devices != nil {
		fmt.Fprintf(os.Stderr, "  %s per-device tokens from %s\n", styler.Paint(ui.Green, "●"), opts.devicesFile)
	}
	if opts.pair {
		if err := printPairOffer(srv, opts, styler); err != nil {
			return err
		}
	}
	return srv.ListenAndServe(ctx)
}

// printPairOffer renders the pairing offer: QR block, then the same URI as
// plain text for copy-paste. Scan if you can, paste if you can't.
func printPairOffer(srv *server.Server, opts options, styler ui.Styler) error {
	caps, err := parsePairCapabilities(opts)
	if err != nil {
		return err
	}
	name := opts.pairName // may be empty; the client names itself on consume
	uri, err := srv.CreatePairOffer(server.NormalizePairHost(opts.addr), name, caps, opts.pairTTL)
	if err != nil {
		return fmt.Errorf("create pairing offer: %w", err)
	}
	qr, err := qrcode.New(uri, qrcode.Medium)
	if err != nil {
		return fmt.Errorf("render QR: %w", err)
	}
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, styler.Bold("Pairing offer — scan with the device you want to add:"))
	fmt.Fprintln(os.Stderr)
	fmt.Fprintln(os.Stderr, qr.ToSmallString(false))
	fmt.Fprintf(os.Stderr, "  or paste this on the client:\n\n    mexaroot pair %s\n", uri)
	fmt.Fprintf(os.Stderr, "\n  expires in %s, single use, grants: %s\n", opts.pairTTL, capsLabel(caps))
	return nil
}

func parsePairCapabilities(opts options) ([]string, error) {
	if opts.pairAll {
		return auth.ExpandCapabilities([]string{auth.GroupAll})
	}
	if opts.pairCaps == "" {
		return []string{}, nil // read-only default
	}
	return auth.ExpandCapabilities(strings.Split(opts.pairCaps, ","))
}

func serve(opts options) error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	registerStop(stop)
	return serveContext(opts, ctx)
}

// stopServe ends a running daemon. The Windows service handler needs it
// because the service control manager's Stop does not raise a signal.
func stopServe() {
	stopMu.Lock()
	stop := serveStop
	stopMu.Unlock()
	if stop != nil {
		stop()
	}
}

func registerStop(stop context.CancelFunc) {
	stopMu.Lock()
	serveStop = stop
	stopMu.Unlock()
}

var (
	stopMu    sync.Mutex
	serveStop context.CancelFunc
)
