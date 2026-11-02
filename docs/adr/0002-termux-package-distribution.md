# 0002. Distribution as a Termux package

Status: Accepted

## Context

Some projects embed their own Termux prefix and host application. That route requires building native code from source and maintaining a fork of the host environment.

## Decision

Mexaroot is distributed as a package installed into the user's existing Termux. An Android companion application may be introduced separately where platform features require it, such as a foreground service or boot integration.

## Consequences

- Installation is a single package command and maintenance stays small.
- Behavior follows Termux releases and install sources.
- Capabilities are limited to what a Termux process can do without a dedicated host.
