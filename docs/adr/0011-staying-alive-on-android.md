# 0011. Staying alive under Android background limits

Status: Accepted

## Context

Android stops background work aggressively. The CPU can sleep while the screen is off, and from Android 12 the system kills child processes of apps, which includes everything Termux runs. A daemon that disappears without notice makes every other feature unreliable. Some of the relevant controls live in device settings that an application cannot change.

## Decision

The daemon holds the Termux wake lock for its whole lifetime, taken and released through `termux-wake-lock` and `termux-wake-unlock`, and continues without it when those commands are missing. Restarting after a crash is delegated to runit through termux-services: `mexarootd -install-service` writes a service that starts disabled, and the user enables it with `sv-enable`. Settings that only the device owner can change, such as the child process restrictions, are documented as prerequisites and not worked around in code.

## Consequences

- The daemon survives screen-off and restarts after a crash without custom supervision code.
- Holding a wake lock increases battery use, and `-wake-lock=false` turns it off.
- The supervisor and wake lock depend on termux-services and the Termux tools being installed.
- Reliability on a given device still depends on its power and process policies.
