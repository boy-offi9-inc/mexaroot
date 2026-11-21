package server

import (
	"container/list"
	"sync"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
)

const (
	// replayCap bounds the output replay buffer per detached session.
	replayCap = 256 * 1024
	// DefaultDetachGrace is how long a detached session survives without
	// an owning connection.
	DefaultDetachGrace = 5 * time.Minute
)

// SessionInfo describes one resumable session for session.list.
type SessionInfo struct {
	Stream   uint32
	Runtime  string
	Shell    string
	Owned    bool // owned by the asking connection
	Buffered int  // bytes waiting in the replay buffer
	Detached bool // no connection owns it right now
}

// detachedSession is a session that may outlive its owning connection.
// Output produced while no connection owns it is buffered for replay.
type detachedSession struct {
	mu      sync.Mutex
	session runtimes.Session
	runtime string
	shell   string

	buffer   []byte
	capacity int

	// owner is the connection currently attached; nil when detached.
	owner      *connection
	detachedAt time.Time
}

func (ds *detachedSession) push(p []byte) {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	ds.buffer = append(ds.buffer, p...)
	if len(ds.buffer) > ds.capacity {
		ds.buffer = ds.buffer[len(ds.buffer)-ds.capacity:]
	}
}

// drain returns and clears the replay buffer.
func (ds *detachedSession) drain() []byte {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	out := ds.buffer
	ds.buffer = nil
	return out
}

// currentOwner returns the connection that owns the session now, if any.
func (ds *detachedSession) currentOwner() *connection {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.owner
}

func (ds *detachedSession) info(owner *connection) SessionInfo {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return SessionInfo{
		Runtime:  ds.runtime,
		Shell:    ds.shell,
		Owned:    ds.owner == owner,
		Detached: ds.owner == nil,
		Buffered: len(ds.buffer),
	}
}

// sessionRegistry tracks resumable sessions keyed by daemon-wide stream
// number. Sessions stay alive for the grace period after their owner
// disconnects, then are closed by the reaper.
type sessionRegistry struct {
	mu    sync.Mutex
	order *list.List // *detachedSession, oldest first
	items map[uint32]*list.Element
	next  uint32
	grace time.Duration
	stop  chan struct{}
	once  sync.Once
}

func newSessionRegistry(grace time.Duration) *sessionRegistry {
	if grace <= 0 {
		grace = DefaultDetachGrace
	}
	return &sessionRegistry{
		order: list.New(),
		items: make(map[uint32]*list.Element),
		grace: grace,
		stop:  make(chan struct{}),
	}
}

func (r *sessionRegistry) allocID() uint32 {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.next++
	return r.next
}

// add registers a newly started session owned by conn.
func (r *sessionRegistry) add(id uint32, session runtimes.Session, runtime, shell string, owner *connection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.items[id] = r.order.PushBack(&detachedSession{
		session:  session,
		runtime:  runtime,
		shell:    shell,
		capacity: replayCap,
		owner:    owner,
	})
}

func (r *sessionRegistry) get(id uint32) *detachedSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	if el, ok := r.items[id]; ok {
		return el.Value.(*detachedSession)
	}
	return nil
}

func (r *sessionRegistry) remove(id uint32) *detachedSession {
	r.mu.Lock()
	defer r.mu.Unlock()
	el, ok := r.items[id]
	if !ok {
		return nil
	}
	r.order.Remove(el)
	delete(r.items, id)
	return el.Value.(*detachedSession)
}

// count reports how many sessions are registered.
func (r *sessionRegistry) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.items)
}

// ownedBy reports whether the session's current owner is conn.
func (ds *detachedSession) ownedBy(conn *connection) bool {
	ds.mu.Lock()
	defer ds.mu.Unlock()
	return ds.owner == conn
}

func (r *sessionRegistry) list(owner *connection) []SessionInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	infos := make([]SessionInfo, 0, len(r.items))
	for el := r.order.Front(); el != nil; el = el.Next() {
		info := el.Value.(*detachedSession).info(owner)
		for id, e := range r.items {
			if e == el {
				info.Stream = id
				break
			}
		}
		infos = append(infos, info)
	}
	return infos
}

// detach releases every session owned by conn. They stay alive in the
// registry until the grace period expires or another connection attaches.
func (r *sessionRegistry) detach(owner *connection) {
	r.mu.Lock()
	defer r.mu.Unlock()
	now := time.Now()
	for el := r.order.Front(); el != nil; el = el.Next() {
		ds := el.Value.(*detachedSession)
		ds.mu.Lock()
		if ds.owner == owner {
			ds.owner = nil
			ds.detachedAt = now
		}
		ds.mu.Unlock()
	}
}

// attach hands the session to a connection. It fails while another
// connection owns it.
func (r *sessionRegistry) attach(id uint32, owner *connection) (*detachedSession, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	el, ok := r.items[id]
	if !ok {
		return nil, false
	}
	ds := el.Value.(*detachedSession)
	ds.mu.Lock()
	defer ds.mu.Unlock()
	if ds.owner != nil && ds.owner != owner {
		return ds, false
	}
	ds.owner = owner
	return ds, true
}

// reap closes sessions detached longer than the grace period. The daemon
// runs one reaper goroutine per server.
func (r *sessionRegistry) reap() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.stop:
			return
		case now := <-ticker.C:
			var expired []*detachedSession
			r.mu.Lock()
			var els []*list.Element
			for el := r.order.Front(); el != nil; el = el.Next() {
				ds := el.Value.(*detachedSession)
				ds.mu.Lock()
				stale := ds.owner == nil && now.Sub(ds.detachedAt) > r.grace
				ds.mu.Unlock()
				if stale {
					els = append(els, el)
				}
			}
			for _, el := range els {
				r.order.Remove(el)
				for id, e := range r.items {
					if e == el {
						delete(r.items, id)
						break
					}
				}
				expired = append(expired, el.Value.(*detachedSession))
			}
			r.mu.Unlock()
			for _, ds := range expired {
				ds.session.Close()
			}
		}
	}
}

func (r *sessionRegistry) start() {
	go r.reap()
}

func (r *sessionRegistry) shutdown() {
	r.once.Do(func() { close(r.stop) })
	r.mu.Lock()
	defer r.mu.Unlock()
	for el := r.order.Front(); el != nil; el = el.Next() {
		el.Value.(*detachedSession).session.Close()
	}
	r.order.Init()
	r.items = make(map[uint32]*list.Element)
}
