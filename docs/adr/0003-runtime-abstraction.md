# 0003. Runtime abstraction

Status: Accepted

## Context

Native Termux, proot-distro, and Proroot all run user workloads, but they differ in performance, architecture support, and release cadence. The packaged proot-distro can trail its upstream repository, and Proroot is arm64-only and evolving quickly.

## Decision

A runtime interface covers listing, command execution, interactive sessions, and filesystem access. Backends implement it for native Termux, proot-distro, and Proroot. Nothing above the interface depends on a specific backend.

## Consequences

- New backends are additive and do not change clients.
- The interface is the common denominator; backend-specific features are exposed as capabilities.
- Backend selection becomes configuration rather than a code path.
