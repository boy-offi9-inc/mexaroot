# 0013. Desktop sessions

Status: Accepted

## Context

The daemon so far targets Termux: the service installer writes a termux-services script, the wake lock is a Termux command, and the README pointed desktop users at the roadmap. Desktop PCs were already able to run the *client* (`mexaroot`) against a phone daemon, but there was no supported way to run the *daemon* on a desktop, or to install either binary as a long-lived service there.

Scope was bounded: no mDNS discovery, no TLS overhaul, no protocol changes. Capability negotiation (ADR 0004) already lets an old client talk to a new daemon.

## Decision

**Build targets.** The client and daemon build for `linux/amd64`, `darwin/arm64`, and `windows/amd64` with no source changes; the only OS-specific code already sits behind existing build tags (`internal/runtimes/hostexec` PTY handling, `cmd/mexaroot` resize handling). Cross-compilation is exercised in CI-style checks rather than gated on a release pipeline.

**Linux service.** A systemd unit ships in `contrib/mexaroot.service` (`Restart=on-failure`, `NoNewPrivileges=yes`, `ExecStart=/usr/local/bin/mexarootd -addr 127.0.0.1:8750`). A dedicated `User=` is documented for system installs; a per-user unit with `loginctl enable-linger` is the alternative. The unit documents both.

**Windows service.** Originally documented with `sc.exe` (`sc create mexarootd binPath=... start=auto`) rather than a `golang.org/x/sys/windows/svc` wrapper, on the reasoning that the daemon runs fine as a plain process. That reasoning overlooked the service control manager's handshake: a plain console program started by `sc create` is killed with error 1053 because it never reports `Running`. ADR 0014 supersedes this decision by implementing the service protocol in the daemon, so the same `sc.exe` recipe now works without a wrapper.

**Client UX.** `mexaroot connect` does not exist and was not added — the client already connects via `-url`/`MEXAROOT_TOKEN` and every desktop flow (exec, push, pull, shell, status, doctor) works against a loopback daemon unchanged. The README documents defaults (`ws://127.0.0.1:8750/ws`), token pairing, and an SSH tunnel for remote hosts.

**Verification.** A loopback test ran the built daemon on `127.0.0.1`, connected the client, ran `exec`, pushed and pulled a file with matching SHA-256 on both ends, and confirmed `status` reports protocol 1 with all capabilities. This is a manual acceptance step, not an automated test, because it needs two built binaries and a listening port; the pieces are covered by the existing package tests.

## Consequences

- No new dependencies; Windows users get a documented recipe instead of compiled-in service support. Superseded for Windows by ADR 0014, which adds the `svc` dependency to make `sc create` actually work.
- The daemon's Android assumptions stay contained: the wake lock degrades to a log line when `termux-wake-lock` is missing, so the same binary runs on Linux, macOS, and Windows.
- Desktop pairing reuses the Android pairing-token story exactly (owner-only file, `-print-token`).
- Discovery and remote exposure remain deliberately out of scope; loopback plus tunnel is the security posture (ADR 0005).
