# 0007. Integration with proot-distro through its command line

Status: Accepted

## Context

proot-distro does not expose a library interface, so its command line is the supported surface. Options differ between releases: some provide `--env` for guest environment variables and others do not, while `login`, `--work-dir`, and the `--` command terminator are available throughout. Installed distributions are stored under `$PREFIX/var/lib/proot-distro/installed-rootfs`, one directory per alias.

## Decision

The proot-distro runtime runs commands with `proot-distro login <alias> [--work-dir <path>] -- <command>` and restricts itself to options present in every supported release. Guest environment variables are applied by running the command through `env` inside the guest. Installed distributions are discovered from the rootfs directory, and each alias becomes a runtime identified by that alias. Aliases are validated before use so they cannot be interpreted as options or escape the rootfs directory.

## Consequences

- One implementation works across proot-distro releases without version detection.
- The guest must provide `env`, which the standard distribution images do.
- Discovery reflects the distributions installed when the daemon starts.
- A change to the on-disk layout is isolated to the discovery code.
