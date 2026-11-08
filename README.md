# Mexaroot

Mexaroot connects Termux, proot environments, and your PC through a single authenticated daemon and command-line client.

Running a command inside a proot distro from your PC, moving files between devices, or keeping a service alive in the background currently means combining SSH, rsync, wake locks, and hand-written proot invocations. Mexaroot replaces that with one protocol and one tool.

## Overview

- **Daemon**: runs in Termux, owns the Android-side lifecycle, authenticates clients, and multiplexes sessions inside a single process.
- **Runtimes**: a uniform interface over native Termux, proot-distro, and Proroot, so clients do not depend on which one is in use.
- **Plugins**: optional capabilities such as keep-alive, file transfer, shell access, and desktop sessions, negotiated per connection.
- **Client**: a command-line tool for the PC and for the device itself.

## Design principles

- One long-lived process instead of many children, to stay within Android's background process limits.
- A versioned wire protocol with capability negotiation, so older and newer peers interoperate.
- Secure by default: loopback binding, pairing tokens, and tunneled remote access.
- Swappable runtimes and transports behind stable interfaces.

## Documentation

- [Architecture decision records](docs/adr/README.md)
- [Wire protocol](protocol/PROTOCOL.md)
- [Contributing](CONTRIBUTING.md)
- [Security policy](SECURITY.md)

## Usage

Start the daemon in Termux:

```
mexarootd
```

On first start the daemon creates a pairing token and stores it with owner-only permissions. Print it to pair a client:

```
mexarootd -print-token
```

Run commands through the client:

```
mexaroot -token <token> runtimes
mexaroot -token <token> exec termux -- uname -a
```

The client also reads the token from `MEXAROOT_TOKEN` or from the file given with `-token-file`.

Installed proot-distro distributions are registered as additional runtimes identified by their alias, for example `mexaroot exec ubuntu -- cat /etc/os-release`.

When a Proroot launcher is on the path, or named by `MEXAROOT_PROROOT`, each distribution is also registered as `<alias>-proroot`, for example `mexaroot exec ubuntu-proroot -- uname -a`.

Open an interactive shell with `mexaroot shell <runtime>`, for example `mexaroot shell ubuntu`. The terminal size, `TERM`, and window resizes are forwarded, and the session ends when the shell exits.

The daemon listens on `127.0.0.1:8750` and refuses other addresses unless `-allow-non-loopback` is set. Reach it from a PC through an SSH tunnel:

```
ssh -p 8022 -L 8750:127.0.0.1:8750 <phone>
```

## Development

Mexaroot requires Go 1.22 or later.

```
go build ./...
go vet ./...
go test ./...
```

Entrypoints live under `cmd/` and internal packages under `internal/`. The version is set at build time:

```
go build -ldflags "-X github.com/boy-offi9-inc/mexaroot/internal/version.Version=1.0.0" ./cmd/mexarootd
```

## Status

Mexaroot is pre-release. The architecture and wire protocol are documented, and the implementation is under active development.

## License

Licensed under the Apache License, Version 2.0. See [LICENSE](LICENSE) and [NOTICE](NOTICE).
