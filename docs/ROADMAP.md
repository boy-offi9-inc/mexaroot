# Roadmap

What exists today: one authenticated daemon, runtimes for native Termux and
the host, proot-distro and Proroot, exec, interactive shell, push and pull
(256 MiB cap, SHA-256 verified), status, doctor, `-json`, shell completion,
wake lock, termux-services install, desktop daemon recipes, a Windows
service implementation, an MCP server,
15 ADRs, CI, a security policy, and tagged releases with binaries for four
platforms. Not yet in place: automated pairing, a companion app. This page
tracks what is open and the order to take it in.

## Open

### Correctness and documentation to verify first

- [x] Windows service install - shipped: `mexarootd.exe` implements the Windows
  service protocol (`golang.org/x/sys/windows/svc`), so the `sc create` recipe
  in the README starts and stops the daemon like any native service instead of
  failing with error 1053; ADR 0013's plain-supervision claim is corrected by
  ADR 0014 (`cmd/mexarootd/service_windows.go`). Runtime verification on a real
  Windows machine is still pending and tracked under "Device verification needed"
- [ ] Windows interactive sessions: ConPTY support does not exist (the Windows
  PTY path returns `ErrPTYUnsupported`, `internal/runtimes/hostexec/pty_windows.go`);
  the README now states that shell is not available on Windows while exec, push,
  pull, status and doctor are. Verify on Windows
- [x] Private vulnerability reporting - enabled in the repository security settings (2026-11-25); `SECURITY.md` already points reporters there. Dependabot alerts are on too.

### Installation

- [ ] Termux package: a recipe for the Termux package repository, or a documented install script, so a phone does not need a Go toolchain
- [ ] Test that a static Linux arm64 build runs correctly on Android (DNS and certificate paths), or build natively in Termux for releases

### Security and pairing

Today there is one shared token, a manual copy step and an SSH tunnel for
remote use.

- [x] Pairing flow: `mexarootd -pair` prints a QR code and the same offer as a `mexaroot://pair?…` URI; the client runs `mexaroot pair <uri>`, which consumes the single-use, TTL-bound 128-bit-class secret over the normal listener and saves the minted device token (scoped to the capabilities chosen at `-pair`) as its default token. Design discussed in #4 (2026-11-28)
- [x] Per-device tokens with listing, revocation and rotation - shipped: `internal/auth/devices.go` store (atomic 0600 JSON, name uniqueness, constant-time verify), `mexarootd -devices` enables the store, `mexarootd devices add|list|revoke|rotate` manages it without stopping the daemon, server accepts shared or device tokens (device-only possible by emptying the shared token file), `device_auth` audit events name the device (2026-11-27)
- [ ] Built-in TLS with a pinned certificate fingerprint, as an alternative to the SSH tunnel
- [x] Remove the need for `-token` on the command line - the env var and `-token-file` have existed for a while; the README now actively recommends them over `-token` (2026-11-25)
- [x] Optional per-runtime or read-only permissions for a token - shipped: device tokens carry a capability whitelist (`exec`, `shell`, `files.read`, `files.write`, `forward`); new devices start read-only (connect + list only), the hello advertises only granted capabilities and the router answers `forbidden` outside the whitelist; legacy nil-whitelist devices keep full access (2026-11-28)

### Features

- [ ] Port forwarding into a runtime (forward to hosts reachable from the daemon device shipped in v0.2.0; forwarding to services inside a distro still needs runtime-level routing)
- [x] Session resume and reattach - shipped: sessions survive their connection for a detach grace window with output buffered while detached, `mexaroot sessions` lists them and `mexaroot attach STREAM` replays the buffer and continues (`session.list` / `session.attach`, `internal/server/sessions_resume.go`, verified E2E with the real daemon)
- [ ] Streaming exec with cancel - live output for long commands
- [ ] Distro lifecycle and hot reload - newly installed distros appear without restarting the daemon, plus list, install and remove
- [ ] Clipboard sync and notifications - small phone-side conveniences, for example a notification when a long command finishes
- [ ] Android companion app (Kotlin) - boot start, foreground service, QR pairing and a status tile; replaces the reliance on Termux:Boot
- [ ] Boot start without the app - Termux:Boot instructions, once the details are verified

## Quality and project health

- [x] Add govulncheck and Dependabot, and pin GitHub Actions to commit SHAs - shipped: govulncheck job in ci.yml (manual run: no vulnerabilities), Dependabot on gomod + github-actions weekly, checkout/setup-go/goreleaser-action pinned
- [x] Run tests on macOS and Windows as well - shipped: the ci.yml test job is a matrix over ubuntu, macOS and Windows runners (gofmt stays Ubuntu-only; its output is identical everywhere). Cross-platform regressions now fail CI instead of waiting for a user report (2026-11-27)
- [x] Fuzz the wire decoder - shipped: `internal/wire/fuzz_test.go` (seed corpus runs in CI; a 60s live run at ~8.5k execs/s found only an over-strict assertion in the fuzz contract itself, no decoder bugs)
- [x] Protocol conformance tests - shipped: `internal/wire/conformance_test.go` locks the protocol contract every peer must satisfy: envelope round trips (v/id/type/ok/stream/payload/error), decode rejections (`CodeInvalidRequest` on malformed JSON, missing type or version), unknown future types still decoding with no capability claim, every declared type mapped to a capability, negotiation semantics (highest common version, empty/no-overlap lists), chunk framing limits and error/event envelopes
- [ ] Add a CI integration test that starts the daemon and drives a real PTY session
- [ ] Publish a compatibility matrix: Android versions, Termux builds, proot-distro and Proroot versions, with the checks done by `mexaroot doctor`
- [x] Issue and pull request templates - shipped (bug report, feature request, PR template with a protocol-impact checklist). Starter issues and the architecture overview still open.
- [ ] Keep the README claims in step with the code

## Suggested order

1. Pairing with TLS and per-device tokens (both correctness items are shipped)
2. Port forwarding into runtimes
3. Companion app
4. Streaming exec with cancel
5. Ongoing quality items

## Device verification needed

- The Windows service recipe and shell on Windows (the service protocol is now implemented; the recipe itself still needs a run on real hardware)
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
