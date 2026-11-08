# 0009. Interactive sessions over pseudo-terminals

Status: Accepted

## Context

Interactive shells need terminal semantics such as echo, line editing, job control, and window size changes, which piping a process's standard streams cannot provide. Sessions also carry many small input and output messages, so keystrokes must never be reordered.

## Decision

Each session runs one process attached to a pseudo-terminal, started through the runtime that owns the environment. The daemon assigns a stream number per session, reads output continuously and emits it as events, and applies input, resize, and close requests in the order they arrive on the connection. A session ends when its process exits or when its connection closes, and the daemon terminates any session still open when a connection ends. Runtimes opt in by implementing an interactive session interface, and the daemon reports the request as unsupported for the others.

## Consequences

- Terminal behavior matches a local terminal, including programs that require a controlling terminal.
- Sessions do not outlive their connection, so detaching and reattaching is not provided.
- Input order is guaranteed per connection, and a stalled session can delay other requests on the same connection.
- Pseudo-terminal support depends on the host platform, so Windows hosts cannot run sessions.
