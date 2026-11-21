# Roadmap

What exists today: one authenticated daemon, runtimes for native Termux and
the host, proot-distro and Proroot, exec, interactive shell, push and pull
(256 MiB cap, SHA-256 verified), status, doctor, `-json`, shell completion,
wake lock, termux-services install, desktop daemon recipes, an MCP server,
13 ADRs, CI, a security policy, and tagged releases with binaries for four
platforms. Not yet in place: automated pairing, port forwarding, session
resume, a companion app. This page tracks what is open and the order to
take it in.

## Open

### Correctness and documentation to verify first

- [ ] Windows service install: the README registers `mexarootd.exe` with `sc create`, but a plain console program normally cannot run under the Windows service manager (fails to start with error 1053), and ADR 0013's claim that plain supervision is enough needs testing - fix by implementing the service protocol (`golang.org/x/sys/windows/svc`), or switch the docs to a wrapper such as NSSM or WinSW, or to Task Scheduler. Verify on Windows
- [ ] Windows interactive sessions: pseudo-terminal support was Unix-only in the original code; the README says desktop sessions work - either ConPTY support exists, or the README states which platforms support shell. Verify on Windows
- [ ] Private vulnerability reporting: `SECURITY.md` tells reporters to use GitHub's private reporting option - enable it in the repository security settings

### Installation

- [ ] Termux package: a recipe for the Termux package repository, or a documented install script, so a phone does not need a Go toolchain
- [ ] Test that a static Linux arm64 build runs correctly on Android (DNS and certificate paths), or build natively in Termux for releases

### Security and pairing

Today there is one shared token, a manual copy step and an SSH tunnel for
remote use.

- [ ] Pairing flow: `mexarootd -pair` shows a QR code or URI that encodes the address and a one-time secret, and the client saves a named host
- [ ] Per-device tokens with listing, revocation and rotation
- [ ] Built-in TLS with a pinned certificate fingerprint, as an alternative to the SSH tunnel
- [ ] Remove the need for `-token` on the command line, since it is visible in the process list (environment variable and file already exist)
- [ ] Optional per-runtime or read-only permissions for a token

### Features

- [ ] Port forwarding into a runtime (forward to hosts reachable from the daemon device shipped in v0.2.0; forwarding to services inside a distro still needs runtime-level routing)
- [ ] Session resume and reattach - shells survive Wi-Fi drops (ADR 0009 documents that sessions currently end with the connection)
- [ ] Streaming exec with cancel - live output for long commands
- [ ] Distro lifecycle and hot reload - newly installed distros appear without restarting the daemon, plus list, install and remove
- [ ] Clipboard sync and notifications - small phone-side conveniences, for example a notification when a long command finishes
- [ ] Android companion app (Kotlin) - boot start, foreground service, QR pairing and a status tile; replaces the reliance on Termux:Boot
- [ ] Boot start without the app - Termux:Boot instructions, once the details are verified

## Quality and project health

- [ ] Add govulncheck and Dependabot, and pin GitHub Actions to commit SHAs
- [ ] Run tests on macOS and Windows as well; the current workflow tests on Ubuntu only, with cross-builds for the other platforms (verify)
- [ ] Fuzz the wire decoder and add protocol conformance tests
- [ ] Add a CI integration test that starts the daemon and drives a real PTY session
- [ ] Publish a compatibility matrix: Android versions, Termux builds, proot-distro and Proroot versions, with the checks done by `mexaroot doctor`
- [ ] Issue and pull request templates, labelled starter issues, and a short architecture overview for contributors
- [ ] Keep the README claims in step with the code

## Suggested order

1. Correctness fixes (Windows service, vulnerability reporting)
2. Pairing with TLS and per-device tokens
3. Port forwarding
4. Session resume
5. Companion app
6. Ongoing quality items

## Device verification needed

- The Windows service recipe and shell on Windows
- A static Linux arm64 binary running in Termux
- Proroot options and the guest environment baseline on real hardware
- Behaviour of the wake lock and runit service across a reboot

## Done

- [x] `go install github.com/boy-offi9-inc/mexaroot/cmd/mexaroot@latest`: verified
  against the v0.1.0 tag, and `internal/version` now falls back to the module's
  build info, so `go install`ed binaries report their real version instead of
  `dev` (release builds keep the ldflags-injected tag; plain checkouts report
  `dev-<commit>`). See `internal/version/version.go`
- [x] MCP server (`mexaroot mcp`): a Model Context Protocol server on stdio so AI
  agents (Claude Desktop, Claude Code, any MCP client) can list runtimes, run
  commands, push and pull files, and read status - through the same daemon and
  token as the CLI, so it adds no new privileges. `-read-only` registers only
  the read-only tools; `-allow` restricts the tool list further. Unknown or
  refused tools come back as tool-level errors, never a crash. Verified
  end-to-end: daemon + `mexaroot mcp` + JSON-RPC over stdio, exec round-trip and
  audit entries recorded. See `internal/mcp/` and `docs/MCP.md`
- [x] Release tooling and the v0.1.0 release: `.goreleaser.yaml` (both binaries,
  linux/amd64, linux/arm64, darwin/arm64, windows/amd64; tar.gz + zip for
  Windows; checksums; version injected via ldflags), `.github/workflows/release.yml`
  (publishes on `v*` tag push), `CHANGELOG.md` (Keep a Changelog), and
  `docs/RELEASING.md` (the per-release checklist). v0.1.0 is tagged and published
  with all four archives plus `checksums.txt`
- [x] Audit log of connections and executed commands (`mexarootd -audit-log FILE`): an
  append-only JSON Lines file (mode 0600) recording `connect`, `auth_failed`, `exec`
  (runtime + argv), `session_start` (runtime) and failures. Off by default; a write
  error is counted and surfaced by the status check rather than breaking the request.
  See `internal/server/audit.go`
- [x] Rate limiting on failed handshakes: five failed handshakes (bad token, malformed
  message, wrong type, undecodable payload) from one IP inside a rolling minute puts the
  host on a 30-second cooldown, during which new WebSocket connections get HTTP 429
  before any frame is read. A successful handshake clears the host's record, and loopback
  addresses are never limited. See `internal/server/handshake_limiter.go`
- [x] Exec output limits: at most 8 MiB per stream (`hostexec.MaxExecOutput`); anything
  past the cap is discarded while the command runs to completion,
  `ExecResult.Truncated` is carried through the wire protocol as `truncated` on
  `exec.run` responses, and the client notes the cut on stderr. Tests cover the cap,
  the exact-fit boundary and a 20 MiB run. See `internal/runtimes/hostexec/capped.go`
- [x] Runtime ID on desktops: the native runtime reported `termux` on Linux, macOS and
  Windows. It now reports `host` (Kind `host`) outside Android - detection is
  `runtime.GOOS == "android"` or `TERMUX_VERSION` set - and keeps `termux` inside
  Termux. See `internal/runtimes/native/`
