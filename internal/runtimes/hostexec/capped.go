package hostexec

import "bytes"

// MaxExecOutput caps how much of one command's stdout or stderr is kept in
// memory, matching the 256 MiB file-transfer cap's spirit but far smaller:
// exec output exists to be read by a human or returned over the wire, and a
// runaway command (yes | true) must not grow the daemon's heap without
// bound. Everything past the cap is discarded while the command still runs
// to completion, so exit codes and timeouts behave exactly as before.
const MaxExecOutput = 8 << 20 // 8 MiB

// cappedBuffer is an io.Writer that keeps the first Max bytes and drops
// the rest, remembering whether anything was dropped.
type cappedBuffer struct {
	buf     bytes.Buffer
	max     int
	dropped bool
}

func newCappedBuffer(max int) *cappedBuffer { return &cappedBuffer{max: max} }

func (c *cappedBuffer) Write(p []byte) (int, error) {
	if c.dropped {
		return len(p), nil
	}
	room := c.max - c.buf.Len()
	if room <= 0 {
		c.dropped = true
		return len(p), nil
	}
	if len(p) > room {
		c.buf.Write(p[:room])
		c.dropped = true
		return len(p), nil
	}
	c.buf.Write(p)
	return len(p), nil
}

func (c *cappedBuffer) truncated() bool { return c.dropped }
