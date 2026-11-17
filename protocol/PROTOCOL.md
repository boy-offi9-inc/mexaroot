# Wire protocol

Version 1

## Transport

Messages are UTF-8 JSON text frames over a WebSocket connection. Bulk file data uses binary frames. The protocol is independent of its transport. The default is WebSocket on a loopback address, reached remotely through an SSH tunnel. The daemon serves the endpoint at `/ws` and listens on `127.0.0.1:8750` by default.

## Envelope

Every text frame is a JSON object.

| Field | Type | Description |
| --- | --- | --- |
| `v` | integer | Protocol version |
| `id` | string | Client-chosen request identifier, echoed in the response |
| `type` | string | Message type |
| `payload` | object | Message body |

A response repeats `id` and `type` and adds `ok`. When `ok` is `true` the body is in `payload`. When `ok` is `false` the body is an `error` object with a `code` and a `message`.

Events are pushed by the daemon without a request. They omit `id` and carry a `stream` field that names the session or transfer they belong to.

## Errors

| Code | Meaning |
| --- | --- |
| `unauthorized` | Missing or invalid token, or no completed handshake |
| `unsupported` | Unknown message type, unavailable capability, or no common protocol version |
| `invalid_request` | Malformed or incomplete payload |
| `not_found` | Unknown runtime, stream, or path |
| `timeout` | The operation exceeded its time limit |
| `internal` | Unexpected daemon failure |

## Handshake

The client opens with `hello`. Any other message before a successful `hello` is rejected with `unauthorized`.

Request payload:

```json
{
  "client": "mexaroot-cli/1.0.0",
  "protocol": [1],
  "token": "..."
}
```

Response payload:

```json
{
  "daemon": "1.0.0",
  "protocol": 1,
  "capabilities": ["exec", "session", "file", "keepalive"]
}
```

The daemon selects the highest protocol version both peers support. If none is shared, it returns `unsupported` with the versions it accepts.

## Capabilities

A capability is a string naming an optional feature. The daemon lists the capabilities it provides in the handshake response, and a client must not send messages that belong to a capability that was not listed.

| Capability | Messages |
| --- | --- |
| `exec` | `exec.run` |
| `session` | `session.start`, `session.input`, `session.resize`, `session.close` |
| `file` | `file.push`, `file.pull` |
| `keepalive` | `ping` |

`runtime.list` is always available.

## Runtimes

`runtime.list` takes an empty payload and returns:

```json
{
  "runtimes": [
    { "id": "termux", "kind": "termux", "name": "Termux", "state": "available" },
    { "id": "ubuntu", "kind": "proot-distro", "name": "Ubuntu", "state": "available" }
  ]
}
```

`kind` is one of `termux`, `proot-distro`, or `proroot`. `state` is `available` or `unavailable`.

## Command execution

`exec.run` runs a command to completion.

Request payload:

```json
{
  "runtime": "ubuntu",
  "argv": ["uname", "-a"],
  "cwd": "/root",
  "env": { "LANG": "C.UTF-8" },
  "timeout_ms": 30000
}
```

`cwd`, `env`, and `timeout_ms` are optional. The response payload contains `exit_code`, `stdout`, and `stderr`.

## Interactive sessions

`session.start` opens a pseudo-terminal in a runtime.

Request payload:

```json
{
  "runtime": "ubuntu",
  "shell": "/bin/bash",
  "cols": 80,
  "rows": 24,
  "env": {}
}
```

`shell` and `env` are optional. The response payload contains the `stream` number of the session.

| Message | Payload |
| --- | --- |
| `session.input` | `stream`, `data` (base64) |
| `session.resize` | `stream`, `cols`, `rows` |
| `session.close` | `stream` |

The daemon emits `session.output` events with `data` (base64) and a `session.exit` event with `exit_code` when the session ends.

## File transfer

File paths are resolved relative to the runtime's file system. The native runtime maps to the host file system, while proot-based runtimes map to their root file system directory. Absolute paths are resolved as they are; relative paths are resolved against the runtime root. A path that escapes the runtime root, such as `../escape`, is rejected with `invalid_request`.

Streams are unsigned 32-bit integers assigned by the daemon. A binary frame carries a 4-byte big-endian stream number followed by the chunk data. Chunk data should not exceed 65536 bytes. A frame with no chunk data marks the end of the stream. A transfer may carry at most 268435456 bytes (256 MiB).

`file.push` request payload:

```json
{
  "runtime": "ubuntu",
  "path": "/root/archive.tar.gz",
  "size": 1048576,
  "sha256": "...",
  "mode": 420
}
```

`size` is the exact byte count to receive and `sha256` the digest of the complete content; both are verified by the daemon. `mode` is optional and carries the file permission bits (default 0644). The response payload contains the `stream` to write to. After the final frame the daemon verifies `size` and `sha256` and emits a `file.done` event with `ok` and, on failure, an `error`. A failed transfer leaves no partial file behind.

`file.pull` request payload:

```json
{
  "runtime": "ubuntu",
  "path": "/root/archive.tar.gz"
}
```

The response payload contains `stream`, `size`, `sha256`, and `mode`. The daemon then sends binary frames on that stream and ends it with an empty frame. A pull of a missing path returns `not_found`; a pull of a directory returns `invalid_request`.

## Keepalive

`ping` takes an empty payload and returns an empty payload. Either peer may close a connection that stops answering.

## Compatibility

Unknown fields in a payload are ignored. Unknown message types return `unsupported`. Additive changes keep the protocol version, and any change that alters existing behavior increments it.

## Security

The daemon binds to the loopback interface by default and rejects every message before a valid `hello`. Remote access is provided by an SSH tunnel or TLS. See [ADR 0005](../docs/adr/0005-secure-by-default-networking.md).
