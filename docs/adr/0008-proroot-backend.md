# 0008. Proroot as an optional backend over proot-distro rootfs

Status: Accepted

## Context

Proroot replaces proot's ptrace-based syscall interception with an in-process LD_PRELOAD runtime, which removes two context switches per system call. It is arm64-only, is distributed as launcher and runtime libraries rather than as a Termux package, and takes the same style of options as proot. It does not manage distributions and does not set up a guest environment.

## Decision

The Proroot runtime reuses the root file systems that proot-distro installs, so one installation serves both backends. It is registered only when a Proroot launcher is found on the path or named by the `MEXAROOT_PROROOT` environment variable. Each distribution appears under the identifier `<alias>-proroot`, next to the proot-distro runtime for the same alias. Because Proroot starts the command with the host environment, the runtime launches it through `env -i` with a fixed baseline, and request variables override that baseline.

## Consequences

- Clients choose a backend per command by choosing the runtime identifier.
- Nothing changes for devices without Proroot, where the runtime is never registered.
- Both backends can operate on the same root file system, so concurrent writes through both are the caller's responsibility.
- Changes to Proroot's options affect only this package.
