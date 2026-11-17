# 0012. Chunked binary frames for file transfer

Status: Accepted

## Context

File transfer needs to move potentially large files over a protocol whose request and response messages are JSON text frames. Base64 inside JSON inflates payloads by a third and would collide with the per-message read limit that protects the daemon. The protocol already reserves binary frames and a `stream` field for bulk data, and defines a 4-byte big-endian stream header with a 65536-byte chunk ceiling.

## Decision

`file.push` and `file.pull` reuse that existing binary chunk format rather than introducing a second encoding. The daemon assigns the stream number, the sender writes 64 KiB chunks, and an empty frame marks the end. Size and SHA-256 are declared up front and verified by the receiver, so a truncated or corrupted transfer is reported instead of silently accepted. Transfers are capped at 256 MiB and four may run concurrently per connection. A path is resolved inside the runtime's file system and any path that escapes it is refused.

## Consequences

- No new framing code: `EncodeChunk` and `DecodeChunk` were written for this format before the first implementation shipped data over it.
- The daemon's JSON read limit continues to guard all control traffic; binary frames carry only chunk bytes, so their size is bounded by the chunk ceiling instead.
- Verification happens at the end of a transfer, so a client that lies about the digest only wastes the transfer, not the integrity of the file.
