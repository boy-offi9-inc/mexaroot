# 0004. Versioned protocol with capability negotiation

Status: Accepted

## Context

Phones and PCs update independently, and optional features should be switchable without breaking compatible peers.

## Decision

Every connection begins with a handshake that negotiates the protocol version and the set of capabilities. Plugins register capabilities, and requests for an unavailable capability return a defined error. The protocol is independent of its transport.

## Consequences

- Older and newer peers interoperate within the negotiated version.
- Plugins can be enabled or disabled per installation.
- The specification must be maintained alongside the implementation.
