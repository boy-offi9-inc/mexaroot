package server

import (
	"net"
	"sync"
	"time"
)

// Handshake failure limiting: a host whose handshake fails (bad token, bad
// protocol, malformed message) is put on cooldown once enough failures pile
// up inside the window, and further connections from it are refused
// immediately while the cooldown lasts. Deliberately simple - a fixed
// threshold, window and cooldown, no escalation to tune - and loopback
// addresses are never limited, so local clients (and tests) are immune.

const (
	// handshakeFailuresToBlock is how many failed handshakes within the
	// window put a host on cooldown.
	handshakeFailuresToBlock = 5
	// handshakeWindow is the rolling window the failures are counted in.
	handshakeWindow = time.Minute
	// handshakeCooldown is how long a blocked host stays blocked.
	handshakeCooldown = 30 * time.Second
)

type handshakeLimiter struct {
	mu     sync.Mutex
	hosts  map[string]*handshakeRecord
	now    func() time.Time
	window time.Duration
}

type handshakeRecord struct {
	failures []time.Time
	until    time.Time
}

func newHandshakeLimiter() *handshakeLimiter {
	return &handshakeLimiter{
		hosts:  make(map[string]*handshakeRecord),
		now:    time.Now,
		window: handshakeWindow,
	}
}

func hostOf(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

func isLoopbackHost(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

// blocked reports whether addr's host is currently on cooldown.
func (l *handshakeLimiter) blocked(addr string) bool {
	host := hostOf(addr)
	if isLoopbackHost(host) {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	rec, ok := l.hosts[host]
	if !ok {
		return false
	}
	return l.now().Before(rec.until)
}

// recordFailure notes a failed handshake from addr and returns whether the
// host should now be considered blocked.
func (l *handshakeLimiter) recordFailure(addr string) bool {
	host := hostOf(addr)
	if isLoopbackHost(host) {
		return false
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	now := l.now()
	rec, ok := l.hosts[host]
	if !ok {
		rec = &handshakeRecord{}
		l.hosts[host] = rec
	}
	// Drop failures that fell out of the window, then add this one.
	keep := rec.failures[:0]
	for _, t := range rec.failures {
		if now.Sub(t) < l.window {
			keep = append(keep, t)
		}
	}
	rec.failures = append(keep, now)
	if len(rec.failures) >= handshakeFailuresToBlock {
		rec.until = now.Add(handshakeCooldown)
		rec.failures = nil
		return true
	}
	return false
}

// success clears a host's record after a successful handshake - a host that
// proves it knows the token gets a fresh slate.
func (l *handshakeLimiter) success(addr string) {
	host := hostOf(addr)
	if isLoopbackHost(host) {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	delete(l.hosts, host)
}
