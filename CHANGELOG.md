# Changelog

All notable changes to mexaroot are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project has not
reached 1.0, so breaking changes are marked directly rather than with major
version bumps.

## Unreleased

### Security
- Per-device capabilities: device tokens carry a whitelist (`exec`, `shell`,
  `files.read`, `files.write`, `forward`) and the message router refuses
  anything outside it with `forbidden`. New devices start read-only; the hello
  advertises exactly what the device may use. `mexarootd devices add|set`
  grants capabilities; existing devices keep full access until narrowed.
- File push and pull now open every path through `os.Root`, so even a symlink
  planted inside a runtime's file system cannot redirect transfers outside the
  root. Path resolution rejects escapes before the syscall as before; the
  kernel now enforces it too. Regression tests cover symlink escapes in both
  directions.
- `ci.yml` grants each job the minimum GitHub token permissions (read-only
  contents) instead of the org default.

### Added
- Pairing flow: `mexarootd -pair` prints a QR code plus the same offer as a
  `mexaroot://pair?host=…&secret=…` URI. `mexaroot pair <uri>` consumes the
  offer (single use, TTL-bound, constant-time checked) and saves the minted,
  capability-scoped device token as the client's default. Pairing
  consumption and failures are audit-logged; the secret never appears in
  logs.
- Per-device tokens: `mexarootd -devices` enables a device token store and
  `mexarootd devices add|list|revoke|rotate` manages it without stopping the
  daemon. A hello may authenticate with the shared token or any live device
  token; pointing `-token-file` at an empty file runs device-only. Device
  authentications appear in the audit log as `device_auth` events with the
  device name.

- Port forwarding: `mexaroot forward LOCAL_PORT HOST PORT` relays local TCP
  connections to a host reachable from the daemon device, over the same
  WebSocket connection. Loopback targets by default; `-forward-allow` on the
  daemon permits other hosts. New `forward` capability and `forward.*` messages
  (documented in `protocol/PROTOCOL.md`).
- Session resume and reattach: sessions survive the connection that started
  them for a grace window (five minutes by default) with output buffered while
  detached. `mexaroot sessions` lists them and `mexaroot attach STREAM`
  replays the buffer and continues the session — shells survive Wi-Fi drops.
  New `session.list` and `session.attach` messages (documented in
  `protocol/PROTOCOL.md`).
- Windows service support: `mexarootd.exe` implements the Windows service
  protocol (`golang.org/x/sys/windows/svc`), so `sc create` starts and stops
  the daemon like any native service instead of failing with error 1053
  (ADR 0014).

### Changed

- Project health: govulncheck runs in CI (plus a manual run today reports no
  vulnerabilities), GitHub Actions are pinned to commit SHAs, Dependabot
  watches Go modules and actions weekly, bug/feature/PR templates exist, and
  the wire decoder is fuzzed (`internal/wire/fuzz_test.go`; the seed corpus
  runs in CI, live fuzzing found and drove out one over-strict round-trip
  assertion, no decoder bugs). The README now recommends `MEXAROOT_TOKEN` or
  `-token-file` over `-token`, which is visible in the process list.
- Dependencies: `golang.org/x/term` v0.47.0 -> v0.48.0 and
  `golang.org/x/sys` v0.49.0 -> v0.50.0. `coder/websocket` and
  `creack/pty` were already at latest. Verified with `go build`, `go vet`
  and the full test suite on Go 1.27.4.

## 0.1.0 - unreleased

The first tagged release: one authenticated daemon bridging Termux, proot
distros and a PC, with a matching client.

### Added

- Runtimes: native (`termux` inside Termux, `host` on desktop), proot-distro
  and Proroot, with `runtimes.list` discovery.
- `exec.run`: run a command in a runtime with cwd, env and timeout; output is
  capped at 8 MiB per stream and responses carry `truncated` when the cap cut
  anything off.
- Interactive PTY sessions (`session.start/input/resize/close`) with
  keepalive; verified on Linux/Termux.
- File transfer (`file.push`/`file.pull`): 256 MiB cap, SHA-256 verified,
  chunked over binary WebSocket frames.
- Wire protocol: length-prefixed JSON messages over WebSocket with protocol
  version negotiation and capability advertisement.
- Auth: shared pairing token (`-print-token`, `MEXAROOT_TOKEN`, token file)
  compared with a constant-time equality check.
- Client CLI (`mexaroot`): exec, shell, push, pull, status, doctor, `-json`
  output, shell completion.
- Daemon CLI (`mexarootd`): `-addr`, `-token-file`, `-allow-non-loopback`,
  `-audit-log FILE` (JSON Lines audit of connections, failed handshakes,
  executed commands and session starts, off by default, mode 0600),
  `-wake-lock`, termux-services install/uninstall.
- Handshake-failure rate limiting: 5 failed handshakes from one IP in a
  rolling minute puts the host on a 30-second cooldown (loopback exempt).
- MCP server (`mexaroot mcp`): expose the runtimes to AI agents over
  stdio, with `-read-only` and an `-allow` allowlist (`docs/MCP.md`).
- Security policy (`SECURITY.md`), 13 ADRs, CI (gofmt, vet, race-tested
  tests, cross-build matrix).
