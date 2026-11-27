package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
	"github.com/boy-offi9-inc/mexaroot/internal/ui"
)

// pairURI is the parsed mexaroot://pair?host=…&secret=…&name=… offer.
type pairURI struct {
	Host   string // scheme + host[:port] the daemon advertises
	Secret string
	Name   string // suggested device name, may be empty
}

func parsePairURI(raw string) (pairURI, error) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return pairURI{}, fmt.Errorf("parse pairing URI: %w", err)
	}
	if u.Scheme != "mexaroot" || u.Host != "pair" {
		return pairURI{}, errors.New("not a mexaroot pairing URI (expected mexaroot://pair?…)")
	}
	q := u.Query()
	host := q.Get("host")
	secret := q.Get("secret")
	if host == "" || secret == "" {
		return pairURI{}, errors.New("pairing URI is missing host or secret")
	}
	return pairURI{Host: host, Secret: secret, Name: q.Get("name")}, nil
}

// pairEndpoint converts the advertised host into the /pair endpoint URL.
func pairEndpoint(host string) (string, error) {
	host = strings.TrimSuffix(host, "/")
	u, err := url.Parse(host)
	if err != nil || u.Host == "" {
		// Bare host:port — assume the daemon's ws:// listener.
		return "http://" + host + "/pair", nil
	}
	switch u.Scheme {
	case "ws":
		u.Scheme = "http"
	case "wss":
		u.Scheme = "https"
	}
	u.Path = strings.TrimSuffix(u.Path, "/ws") + "/pair"
	return u.String(), nil
}

type pairAnswer struct {
	Token        string   `json:"token"`
	Name         string   `json:"name"`
	Capabilities []string `json:"capabilities"`
}

func pairCommand(ctx context.Context, args []string, out, errOut ui.Styler) (int, error) {
	stdout, stderr := os.Stdout, os.Stderr
	fs := flag.NewFlagSet("mexaroot pair", flag.ContinueOnError)
	fs.SetOutput(stderr)
	name := fs.String("name", "", "device name to register (default: the name suggested in the offer, else this machine's hostname)")
	save := fs.String("save", "", "write the token to this file (default: the client's standard token file, mode 0600)")
	noSave := fs.Bool("no-save", false, "only print the token, do not save it")
	timeout := fs.Duration("timeout", 30*time.Second, "how long to wait for the daemon's answer")
	if err := fs.Parse(flagsFirstPair(args, map[string]bool{"name": true, "save": true, "timeout": true})); err != nil {
		return 2, err
	}
	if fs.NArg() != 1 {
		fmt.Fprintln(stderr, "usage: mexaroot pair mexaroot://pair?host=…&secret=… [-name NAME] [-save FILE] [-no-save]")
		return 2, nil
	}
	parsed, err := parsePairURI(fs.Arg(0))
	if err != nil {
		return 1, err
	}
	if *name == "" {
		*name = parsed.Name
	}
	if *name == "" {
		host, _ := os.Hostname()
		*name = host
	}

	endpoint, err := pairEndpoint(parsed.Host)
	if err != nil {
		return 1, err
	}
	body, _ := json.Marshal(map[string]string{"secret": parsed.Secret, "name": *name})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return 1, err
	}
	req.Header.Set("Content-Type", "application/json")
	ctx, cancel := context.WithTimeout(ctx, *timeout)
	defer cancel()
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 1, fmt.Errorf("reach the daemon at %s: %w (is it running, and are you on the same tunnel?)", endpoint, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		var text bytes.Buffer
		_, _ = text.ReadFrom(resp.Body)
		msg := strings.TrimSpace(text.String())
		if msg == "" {
			msg = resp.Status
		}
		return 1, fmt.Errorf("pairing refused: %s", msg)
	}
	var answer pairAnswer
	if err := json.NewDecoder(resp.Body).Decode(&answer); err != nil {
		return 1, fmt.Errorf("decode pairing answer: %w", err)
	}

	fmt.Fprintf(stdout, "paired as %q\n", answer.Name)
	fmt.Fprintf(stdout, "capabilities: %s\n", capsLabelClient(answer.Capabilities))

	if !*noSave {
		path := *save
		if path == "" {
			defaultPath, err := auth.DefaultPath()
			if err != nil {
				return 1, err
			}
			path = defaultPath
		}
		if err := saveTokenFile(path, answer.Token); err != nil {
			return 1, err
		}
		fmt.Fprintf(stdout, "token saved to %s — try: mexaroot runtimes\n", path)
	} else {
		fmt.Fprintf(stdout, "\n%s\n", answer.Token)
	}
	return 0, nil
}

// flagsFirstPair hoists flag tokens ahead of positionals so
// "mexaroot pair mexaroot://… -name phone" parses — Go's flag package
// stops at the first non-flag argument otherwise.
func flagsFirstPair(args []string, valued map[string]bool) []string {
	var flags, rest []string
	for i := 0; i < len(args); i++ {
		if strings.HasPrefix(args[i], "-") {
			flags = append(flags, args[i])
			if valued[strings.TrimLeft(args[i], "-")] && i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
			continue
		}
		rest = append(rest, args[i])
	}
	return append(flags, rest...)
}

func saveTokenFile(path, token string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create token directory: %w", err)
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(token+"\n"), 0o600); err != nil {
		return fmt.Errorf("write token: %w", err)
	}
	if err := os.Rename(tmp, path); err != nil {
		return fmt.Errorf("replace token: %w", err)
	}
	return nil
}

func capsLabelClient(caps []string) string {
	if len(caps) == 0 {
		return "read-only"
	}
	return strings.Join(caps, ",")
}
