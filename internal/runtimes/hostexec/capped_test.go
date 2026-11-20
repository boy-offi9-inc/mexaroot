package hostexec

import (
	"strings"
	"testing"
)

func TestCappedBufferKeepsFirstBytes(t *testing.T) {
	c := newCappedBuffer(8)
	n, err := c.Write([]byte("abcdefghij"))
	if n != 10 || err != nil {
		t.Fatalf("Write: n=%d err=%v", n, err)
	}
	if c.buf.String() != "abcdefgh" {
		t.Fatalf("kept %q", c.buf.String())
	}
	if !c.truncated() {
		t.Fatal("expected truncated")
	}
}

func TestCappedBufferMultipleWrites(t *testing.T) {
	c := newCappedBuffer(10)
	for i := 0; i < 5; i++ {
		if _, err := c.Write([]byte("abcd")); err != nil {
			t.Fatalf("Write: %v", err)
		}
	}
	if c.buf.Len() != 10 {
		t.Fatalf("kept %d bytes, want 10", c.buf.Len())
	}
	if !c.truncated() {
		t.Fatal("expected truncated")
	}
}

func TestCappedBufferExactFitNotTruncated(t *testing.T) {
	c := newCappedBuffer(4)
	if _, err := c.Write([]byte("abcd")); err != nil {
		t.Fatalf("Write: %v", err)
	}
	if c.truncated() {
		t.Fatal("an exact fit must not count as truncated")
	}
}

func TestExecOutputCapped(t *testing.T) {
	// The protocol is new: the daemon previously buffered without a limit,
	// so this is the documented-behaviour test the roadmap item asked for.
	res, err := Run(t.Context(), Command{
		Name: "sh",
		Args: []string{"-c", "dd if=/dev/zero bs=1M count=20 2>/dev/null; printf done >&2"},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Stdout) != MaxExecOutput {
		t.Fatalf("stdout kept %d bytes, want the %d cap", len(res.Stdout), MaxExecOutput)
	}
	if !res.Truncated {
		t.Fatal("expected Truncated on a 20 MiB stdout")
	}
	if string(res.Stderr) != "done" {
		t.Fatalf("stderr: %q", res.Stderr)
	}
}

func TestExecOutputUnderCapNotTruncated(t *testing.T) {
	res, err := Run(t.Context(), Command{
		Name: "sh",
		Args: []string{"-c", "printf " + strings.Repeat("x", 1000)},
	})
	if err != nil {
		t.Fatalf("Run: %v", err)
	}
	if len(res.Stdout) != 1000 || res.Truncated {
		t.Fatalf("stdout=%d bytes truncated=%v", len(res.Stdout), res.Truncated)
	}
}
