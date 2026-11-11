package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/native"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/prootdistro"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes/proroot"
	"github.com/boy-offi9-inc/mexaroot/internal/server"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
	"github.com/boy-offi9-inc/mexaroot/internal/version"
)

const defaultAddr = "127.0.0.1:8750"

func main() {
	addr := flag.String("addr", defaultAddr, "address to listen on")
	tokenFile := flag.String("token-file", "", "path to the pairing token file")
	allowRemote := flag.Bool("allow-non-loopback", false, "permit listening on non-loopback addresses")
	printToken := flag.Bool("print-token", false, "print the pairing token and exit")
	noColor := flag.Bool("no-color", false, "disable colored output")
	showVersion := flag.Bool("version", false, "print the version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Version)
		return
	}

	token, err := loadToken(*tokenFile)
	if err != nil {
		log.Fatalf("mexarootd: %v", err)
	}
	if *printToken {
		fmt.Println(token)
		return
	}

	if err := serve(*addr, token, *allowRemote, *noColor); err != nil {
		log.Fatalf("mexarootd: %v", err)
	}
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

func serve(addr, token string, allowRemote, noColor bool) error {
	styler := ui.New(os.Stderr, noColor)
	fmt.Fprint(os.Stderr, "\n", styler.Banner("mexarootd", version.Version, "Bridging Termux, proot and your PC"))
	fmt.Fprintln(os.Stderr)

	registry, err := buildRegistry()
	if err != nil {
		return err
	}

	srv, err := server.New(server.Config{
		Addr:             addr,
		Token:            token,
		Version:          version.Version,
		Registry:         registry,
		AllowNonLoopback: allowRemote,
	})
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	fmt.Fprintf(os.Stderr, "\n  %s listening on %s\n\n", styler.Paint(ui.Green, "●"), styler.Bold(addr))
	return srv.ListenAndServe(ctx)
}
