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

Running `mexaroot shell` without a runtime opens a numbered menu of the available runtimes. Output is colored on terminals and plain when piped. Pass `-no-color` or set `NO_COLOR` to disable colors, and `FORCE_COLOR` to keep them in pipes.

`mexaroot status` shows the daemon version, negotiated protocol, capabilities, latency, and runtime counts. `mexaroot doctor` checks the token, address, handshake, capabilities, and runtimes in turn and suggests a fix for each problem. Add `-json` to `runtimes`, `exec`, `status`, or `doctor` for machine-readable output, and load completions with `source <(mexaroot completion bash)`; zsh and fish are supported as well.

The daemon listens on `127.0.0.1:8750` and refuses other addresses unless `-allow-non-loopback` is set. Reach it from a PC through an SSH tunnel:

```
ssh -p 8022 -L 8750:127.0.0.1:8750 <phone>
```

## Keeping the daemon running

The daemon holds a Termux wake lock while it runs, so the CPU stays awake with the screen off. Use `-wake-lock=false` to turn that off.

To restart it automatically after a crash, run it under termux-services:

```
pkg install termux-services
mexarootd -install-service
sv-enable mexarootd
```

Restart Termux after installing termux-services so its service daemon starts. The service is installed disabled, `sv-disable mexarootd` stops it, and its logs are in `$PREFIX/var/log/sv/mexarootd/current`. `mexarootd -uninstall-service` removes it once it is disabled.

Android 12 and later also kill background child processes. On Android 14 and newer, turn on Developer options, then Disable child process restrictions. On Android 12L and 13, run this on a computer with USB debugging enabled:

```
adb shell settings put global settings_enable_monitor_phantom_procs false
```

On Android 12, Termux's documentation lists these commands instead:

```
adb shell "/system/bin/device_config set_sync_disabled_for_tests persistent"
adb shell "/system/bin/device_config put activity_manager max_phantom_processes 2147483647"
```

Also set Termux's battery usage to unrestricted in the system settings.

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
