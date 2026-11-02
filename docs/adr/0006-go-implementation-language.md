# 0006. Go as the implementation language

Status: Accepted

## Context

The daemon runs on arm64 Android inside Termux, while the client runs on desktop systems that include Linux, macOS, and Windows. The daemon multiplexes many concurrent sessions and transfers inside one process, and it runs under tight background restrictions where memory and process count matter.

## Decision

The daemon and the client are implemented in Go. The standard library is preferred, and third-party dependencies are added only where they replace substantial, security-sensitive code. The wire protocol remains language-neutral, so other clients can exist.

## Consequences

- Both ends ship as a single static executable with no interpreter or dependency installation.
- Goroutines map directly onto the session and transfer multiplexing described in ADR 0001.
- Pure Go code cross-compiles to every supported target. Features that require cgo need a toolchain for each target.
- Resolver and certificate behavior on Android differs from generic Linux, so network code is tested under Termux and not only on a Linux build.
- Contributors need familiarity with Go.
