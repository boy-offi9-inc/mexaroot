# 0001. Single daemon process

Status: Accepted

## Context

Termux runs shells and background tasks as child processes of its application process. Android classifies these as phantom processes. Android 12 and later limits them system-wide and may terminate them, and newer releases can end long-running processes even when a wake lock is held and battery optimization is disabled.

## Decision

The daemon runs as one long-lived process that multiplexes sessions, executions, and transfers internally. It stays attached to the foreground of its Termux task. An optional helper can disable the phantom process killer through adb, Shizuku, or root.

## Consequences

- Fewer child processes per feature, which keeps the daemon within Android's limits.
- Scheduling and isolation between sessions become the daemon's responsibility.
- Reliability still depends on device policy, so the helper and its documentation are part of the product.
