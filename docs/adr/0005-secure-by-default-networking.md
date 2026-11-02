# 0005. Secure by default networking

Status: Accepted

## Context

A daemon that executes commands and moves files is a high-value target for anyone on the same network.

## Decision

The daemon binds to the loopback interface by default and requires a pairing token on every connection. Remote access goes through an SSH tunnel or TLS. Listening on other interfaces requires explicit configuration and an authenticated, encrypted transport. Tokens are stored with owner-only permissions.

## Consequences

- Remote use takes one extra step.
- Accidental exposure on shared networks is avoided.
- Token lifecycle and rotation are part of the supported surface.
