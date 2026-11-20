<div align="center">

<img src="docs/assets/logo.svg" alt="mexaroot — one daemon, every runtime, your phone" width="560" />

[![CI](https://img.shields.io/github/actions/workflow/status/boy-offi9-inc/mexaroot/ci.yml?branch=main&style=flat-square&label=CI&logo=githubactions&logoColor=white)](https://github.com/boy-offi9-inc/mexaroot/actions/workflows/ci.yml)
[![Go](https://img.shields.io/badge/Go-1.22%2B-111111?style=flat-square&logo=go&logoColor=00ADD8)](go.mod)
[![License](https://img.shields.io/badge/License-Apache%202.0-111111?style=flat-square)](LICENSE)
![Platform](https://img.shields.io/badge/Platform-Termux%20·%20Android%20·%20Linux%20·%20macOS%20·%20Windows-111111?style=flat-square)

**One authenticated daemon linking Termux, proot distros, and your PC.**

`exec` · `shell` · `push` / `pull` · `status` · `doctor` — over one protocol, through one process.

</div>

---

Running a command inside a proot distro from your PC, moving files between devices, or keeping a service alive in the background usually means combining SSH, rsync, wake locks, and hand-written proot invocations. Mexaroot replaces all of that with **one protocol and one tool**.

```
PC ──SSH tunnel──▶ mexarootd (Termux)
                     ├─ termux (native, Android)
                     ├─ host (native, desktop)
                     ├─ ubuntu (proot-distro)
                     ├─ ubuntu-proroot (Proroot)
                     └─ …any runtime that registers
```

## Why it looks like this

- **One long-lived process** instead of many children — respects Android's background process limits instead of fighting them.
- **A versioned wire protocol** with capability negotiation — older and newer peers interoperate.
- **Secure by default** — loopback binding, pairing tokens with constant-time comparison and owner-only file permissions, tunneled remote access. No shell interpolation anywhere.
- **Swappable runtimes** behind stable interfaces — native Termux, proot-distro, and Proroot today; more tomorrow.

## Install

Build from source (Go 1.22+):

```sh
git clone https://github.com/boy-offi9-inc/mexaroot.git
cd mexaroot
go build -o mexarootd ./cmd/mexarootd
go build -o mexaroot ./cmd/mexaroot
```

Copy `mexarootd` wherever you want the daemon to run (phone or desktop) and `mexaroot` wherever you'll drive it from. Client builds are checked for `linux/amd64`, `darwin/arm64`, and `windows/amd64`:

```sh
GOOS=linux   GOARCH=amd64 go build -o mexaroot ./cmd/mexaroot
GOOS=darwin  GOARCH=arm64 go build -o mexaroot ./cmd/mexaroot
GOOS=windows GOARCH=amd64 go build -o mexaroot.exe ./cmd/mexaroot
```

## Desktop install

The daemon is the same binary on every OS. It is long-lived, binds `127.0.0.1:8750` by default, and runs the native host runtime. Clients connect with `mexaroot -url ws://127.0.0.1:8750/ws` and a pairing token from `mexarootd -print-token` (or `MEXAROOT_TOKEN`).

Pass `-audit-log FILE` to have the daemon append a JSON Lines audit log (created with mode `0600`) of connections, failed handshakes, executed commands (runtime and argv) and interactive-session starts. It is off by default, and a failed write never breaks the request it was recording.

### Linux

Build both binaries, install them under `/usr/local/bin`, then install the shipped systemd unit. For a system-wide service that survives reboots:

```sh
sudo install -m 0755 mexarootd mexaroot /usr/local/bin/
sudo cp contrib/mexaroot.service /etc/systemd/system/mexaroot.service
# edit User= to the account that should own the pairing token
sudo systemctl daemon-reload
sudo systemctl enable --now mexaroot
```

`contrib/mexaroot.service` sets `Restart=on-failure`, `NoNewPrivileges=yes`, and `ExecStart=/usr/local/bin/mexarootd -addr 127.0.0.1:8750`. Check logs with `journalctl -u mexaroot -f`. For a per-user service with no root, copy the same unit into `~/.config/systemd/user/mexaroot.service`, drop the `User=` line, then run `systemctl --user enable --now mexaroot` and `loginctl enable-linger "$USER"` so it starts at boot without a login session.

### macOS

Build for `darwin/arm64` and install to `/usr/local/bin`. There is no systemd unit for macOS; a launchd agent in `~/Library/LaunchAgents/` is the equivalent. Create `~/Library/LaunchAgents/dev.mexaroot.mexarootd.plist` with `ProgramArguments` pointing at `/usr/local/bin/mexarootd -addr 127.0.0.1:8750`, `RunAtLoad` true, and `KeepAlive` true, then load it:

```sh
launchctl load ~/Library/LaunchAgents/dev.mexaroot.mexarootd.plist
```

The agent starts on login and restarts on failure. Pair and connect exactly as on Linux; the loopback default and token file path are unchanged.

### Windows

Build `mexaroot.exe` and `mexarootd.exe` with `GOOS=windows GOARCH=amd64`, then register the daemon with Service Control Manager from an elevated prompt:

```bat
sc create mexarootd binPath= "C:\Program Files\mexaroot\mexarootd.exe -addr 127.0.0.1:8750" start= auto
sc start mexarootd
sc query mexarootd
```

`start= auto` survives reboot, and Windows restarts the service on failure through its default recovery policy (set explicitly with `sc failure mexarootd reset= 0 actions= restart/5000` if needed). Remove it with `sc stop mexarootd && sc delete mexarootd`. No `svc` wrapper ships; plain process supervision is enough (ADR 0013). Client commands are the same from PowerShell, e.g. `mexaroot.exe exec host -- hostname` (the native runtime is `host` outside Android, `termux` inside Termux).

On all three, a desktop daemon is reachable from another machine through the same SSH tunnel documented below; loopback binding is kept by default.

## Quick start

Start the daemon in Termux:

```sh
mexarootd
```

First start creates a pairing token and stores it owner-only. Print it to pair a client:

```sh
mexarootd -print-token
```

Then from the PC (through an SSH tunnel — the daemon listens on `127.0.0.1:8750` and refuses anything else unless `-allow-non-loopback` is set):

```sh
ssh -p 8022 -L 8750:127.0.0.1:8750 <phone>
```

```sh
export MEXAROOT_TOKEN=<token>

mexaroot runtimes                              # what's registered
mexaroot exec host -- uname -a                # run a command (termux inside Termux on Android)
mexaroot shell ubuntu                          # interactive shell (PTY, resize, TERM forwarded)
mexaroot push ubuntu report.pdf /root/         # file in
mexaroot pull ubuntu /root/out.log out.log     # file out
mexaroot status                                # version, protocol, capabilities, latency
mexaroot doctor                                # checks each link in the chain, suggests fixes
```

Shell with no runtime argument opens a numbered menu. `-json` works on `runtimes`, `exec`, `status`, and `doctor`. Completions: `source <(mexaroot completion bash)` (zsh and fish too). Colors follow the terminal, with `-no-color` / `NO_COLOR` / `FORCE_COLOR` to override.

**File transfer details:** remote paths resolve *inside the runtime's filesystem* (`/root/…` is the distro's root, not Android's), escapes are refused, and every transfer is size- and SHA-256-verified by both ends. Cap: 256 MiB. The token can also come from `-token <token>` or `-token-file <path>`.

Installed proot-distro distros register automatically by alias (`mexaroot exec ubuntu -- cat /etc/os-release`); with a Proroot launcher on the path, each also registers as `<alias>-proroot`.

**Exec output limit:** `exec` keeps at most 8 MiB of stdout and 8 MiB of stderr in memory; anything past that is discarded while the command still runs to completion (exit codes and timeouts unaffected). When output was cut, the response carries `truncated: true` (visible in `-json`) and the client prints a note on stderr — a runaway `yes` can't grow the daemon's heap.

## Keeping the daemon alive

The daemon holds a Termux wake lock while it runs (`-wake-lock=false` to disable). For automatic restart after a crash, run it under termux-services:

```sh
pkg install termux-services
mexarootd -install-service
sv-enable mexarootd
```

Restart Termux once after installing termux-services. The service installs disabled; `sv-disable mexarootd` stops it, logs live in `$PREFIX/var/log/sv/mexarootd/current`, and `mexarootd -uninstall-service` removes it.

Android 12+ may also kill background child processes:

- **Android 14+**: Developer options → *Disable child process restrictions*.
- **Android 12L/13** (USB debugging from a computer):

  ```sh
  adb shell settings put global settings_enable_monitor_phantom_procs false
  ```

- **Android 12**:

  ```sh
  adb shell "/system/bin/device_config set_sync_disabled_for_tests persistent"
  adb shell "/system/bin/device_config put activity_manager max_phantom_processes 2147483647"
  ```

Also set Termux's battery usage to *Unrestricted* in system settings.

## Documentation

| Doc | Covers |
|---|---|
| [Wire protocol](protocol/PROTOCOL.md) | Message shapes, handshake, capability negotiation, chunked file transfer |
| [ADR index](docs/adr/README.md) | Thirteen architecture decisions and why each was made |
| [Systemd unit](contrib/mexaroot.service) | Reboot-survivable desktop daemon install on Linux |
| [Security policy](SECURITY.md) | How to report vulnerabilities |
| [Contributing](CONTRIBUTING.md) | House style and workflow |

## Development

```sh
go build ./...
go vet ./...
go test -race ./...
```

Entrypoints live under `cmd/`, internal packages under `internal/`. The version is injected at build time:

```sh
go build -ldflags "-X github.com/boy-offi9-inc/mexaroot/internal/version.Version=1.0.0" ./cmd/mexarootd
```

## Status

Pre-release, actively developed. The architecture, wire protocol, file transfer, and desktop daemon installs (systemd unit, launchd agent, `sc.exe` recipe) are implemented and tested (race-detector clean across 15 packages); desktop sessions work against the loopback daemon today.

## License

Apache License 2.0 — see [LICENSE](LICENSE) and [NOTICE](NOTICE).
