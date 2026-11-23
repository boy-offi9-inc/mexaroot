# 0014. Windows service protocol

Status: Accepted (supersedes the Windows-service decision in ADR 0013)

## Context

ADR 0013 documented running the Windows daemon as an `sc.exe` service with plain
process supervision, on the reasoning that the daemon "runs fine as a plain
process". That reasoning was wrong in a specific way: `sc create` starts the
program through the Windows service control manager, which expects a program
that talks the service protocol (the `SERVICE_TABLE_ENTRY` handshake). A plain
console program never performs that handshake, so the service manager kills it
about thirty seconds after start with error 1053 ("The service did not respond
to the start or control request in a timely fashion"). The `sc create` recipe
in the README therefore could not have worked, and the claim that plain
supervision is enough was untested.

The alternative is to drop `sc.exe` and document a service wrapper such as
NSSM or WinSW, or a scheduled task. That keeps the daemon free of Windows-only
code but adds a third-party download to every Windows install and still runs
the daemon as a child of the wrapper rather than as the service itself.

## Decision

The daemon implements the Windows service protocol with
`golang.org/x/sys/windows/svc`. When `mexarootd.exe` is started by the service
control manager it runs the `svc.Run` handler: it reports `StartPending` and
then `Running`, keeps serving requests on the configured address, and answers
`Stop` and `Shutdown` by cancelling the daemon loop and reporting that it
is stopping. The handler runs the same daemon loop as a console invocation, so
there is one code path and no separate service binary.

When started from a terminal, the process behaves exactly as before; the
service handshake only engages when the process detects that it was launched
by the service manager (`svc.IsWindowsService`). The `sc create` recipe from
ADR 0013 is unchanged as a command; what changes is that it now works.

## Consequences

- `golang.org/x/sys/windows` becomes a dependency, used only in a
  Windows-tagged file; Linux, macOS and Android builds are unaffected.
- `sc failure mexarootd reset= 0 actions= restart/5000` now means something:
  the service manager can restart the daemon because the daemon is the service.
- The error-1053 risk moves from "likely broken" to covered by the protocol
  implementation; runtime verification on a real Windows machine is still
  pending and tracked in the roadmap.
- ADR 0013's Windows-service decision is superseded by this record.
