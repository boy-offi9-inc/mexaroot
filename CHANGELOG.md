# Changelog

All notable changes to mexaroot are documented here. The format is based on
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/); the project has not
reached 1.0, so breaking changes are marked directly rather than with major
version bumps.

## Unreleased

Nothing yet.

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
