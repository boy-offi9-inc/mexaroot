package server

import (
	"testing"
	"time"
)

func TestLoopbackIsNeverLimited(t *testing.T) {
	l := newHandshakeLimiter()
	for _, addr := range []string{"127.0.0.1:5000", "[::1]:5000", "localhost:5000"} {
		for i := 0; i < handshakeFailuresToBlock+3; i++ {
			if l.recordFailure(addr) {
				t.Fatalf("%s: loopback host reported blocked after %d failures", addr, i+1)
			}
			if l.blocked(addr) {
				t.Fatalf("%s: loopback host reported blocked", addr)
			}
		}
	}
}

func TestBlocksAfterThreshold(t *testing.T) {
	l := newHandshakeLimiter()
	addr := "203.0.113.9:4444"
	for i := 0; i < handshakeFailuresToBlock-1; i++ {
		if l.recordFailure(addr) {
			t.Fatalf("blocked early, at failure %d of %d", i+1, handshakeFailuresToBlock)
		}
	}
	if !l.recordFailure(addr) {
		t.Fatalf("expected the %dth failure to block the host", handshakeFailuresToBlock)
	}
	if !l.blocked(addr) {
		t.Fatal("host should be on cooldown after the threshold")
	}
}

func TestFailuresOutsideWindowDoNotAccumulate(t *testing.T) {
	l := newHandshakeLimiter()
	addr := "203.0.113.10:4444"
	now := time.Unix(1_000_000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < handshakeFailuresToBlock; i++ {
		if l.recordFailure(addr) {
			t.Fatalf("blocked unexpectedly at failure %d", i+1)
		}
		// A window passes between each failure, so each drops the earlier ones.
		now = now.Add(l.window + time.Second)
	}
	if l.blocked(addr) {
		t.Fatal("failures spread across windows should never block")
	}
}

func TestSuccessClearsTheRecord(t *testing.T) {
	l := newHandshakeLimiter()
	addr := "203.0.113.11:4444"
	for i := 0; i < handshakeFailuresToBlock-1; i++ {
		l.recordFailure(addr)
	}
	l.success(addr)
	if l.recordFailure(addr) {
		t.Fatal("a successful handshake should reset the count, not block on the next failure")
	}
	if l.blocked(addr) {
		t.Fatal("record should have been cleared")
	}
}

func TestCooldownExpires(t *testing.T) {
	l := newHandshakeLimiter()
	addr := "203.0.113.12:4444"
	now := time.Unix(2_000_000, 0)
	l.now = func() time.Time { return now }
	for i := 0; i < handshakeFailuresToBlock; i++ {
		l.recordFailure(addr)
	}
	if !l.blocked(addr) {
		t.Fatal("expected the host to be blocked")
	}
	now = now.Add(handshakeCooldown + time.Second)
	if l.blocked(addr) {
		t.Fatal("cooldown should have expired")
	}
}
