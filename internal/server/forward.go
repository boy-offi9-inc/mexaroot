package server

import (
	"context"
	"encoding/base64"
	"fmt"
	"net"
	"time"

	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const (
	maxForwards   = 8
	forwardBuffer = 32 * 1024
	dialTimeout   = 5 * time.Second
)

// forwardChannel is one open TCP connection on the daemon side, identified by
// the same stream id the client uses for its binary chunks.
type forwardChannel struct {
	conn net.Conn
	done chan struct{}
}

// handleForwardOpen dials the requested host:port and relays it on a new stream.
func (c *connection) handleForwardOpen(ctx context.Context, req wire.Message) {
	var params wire.ForwardOpenRequest
	if err := req.DecodePayload(&params); err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}
	if params.Port <= 0 || params.Port > 65535 {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: "port must be 1-65535"}))
		return
	}
	if err := c.server.checkForwardTarget(params.Host); err != nil {
		c.server.audit.write(AuditEntry{Event: "forward_open", RemoteIP: hostOf(c.remoteIP), Error: err.Error()})
		c.send(ctx, errorResponse(req, err))
		return
	}

	stream, ok := c.addForward()
	if !ok {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: "too many open forwarded channels"}))
		return
	}

	conn, err := net.DialTimeout("tcp", net.JoinHostPort(params.Host, fmt.Sprint(params.Port)), dialTimeout)
	if err != nil {
		c.removeForward(stream)
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeNotFound, Message: "cannot reach " + params.Host + ": " + err.Error()}))
		return
	}

	ch := &forwardChannel{conn: conn, done: make(chan struct{})}
	c.mu.Lock()
	c.forwards[stream] = ch
	c.mu.Unlock()

	c.server.audit.write(AuditEntry{Event: "forward_open", RemoteIP: hostOf(c.remoteIP), Detail: fmt.Sprintf("%s:%d", params.Host, params.Port)})

	resp, err := wire.NewResponse(req, wire.ForwardOpenResponse{Stream: stream})
	if err != nil {
		c.closeForward(stream)
		c.send(ctx, errorResponse(req, err))
		return
	}
	c.send(ctx, resp)

	c.wg.Add(1)
	go c.pumpForwardOut(ctx, stream, ch)
}

// pumpForwardOut reads the daemon-side TCP connection and pushes forward.data
// events to the client until the connection ends.
func (c *connection) pumpForwardOut(ctx context.Context, stream uint32, ch *forwardChannel) {
	defer c.wg.Done()
	defer c.closeForward(stream)

	buf := make([]byte, forwardBuffer)
	for {
		n, err := ch.conn.Read(buf)
		if n > 0 {
			ev, everr := wire.NewEvent(wire.TypeForwardData, stream, wire.SessionOutput{Data: base64.StdEncoding.EncodeToString(buf[:n])})
			if everr != nil {
				return
			}
			c.send(ctx, ev)
		}
		if err != nil {
			// Connection closed or read error: tell the client the channel ended.
			c.sendEvent(ctx, wire.TypeForwardClosed, stream, wire.ForwardClose{})
			return
		}
		select {
		case <-ch.done:
			return
		default:
		}
	}
}

// forwardData routes client bytes to the daemon-side connection.
func (c *connection) forwardData(ctx context.Context, req wire.Message) (wire.Message, error) {
	var payload wire.SessionOutput
	if err := req.DecodePayload(&payload); err != nil {
		return wire.Message{}, err
	}
	if req.Stream == nil {
		return wire.Message{}, &wire.Error{Code: wire.CodeInvalidRequest, Message: "stream is required"}
	}
	stream := *req.Stream
	c.mu.Lock()
	ch, ok := c.forwards[stream]
	c.mu.Unlock()
	if !ok {
		return wire.Message{}, &wire.Error{Code: wire.CodeNotFound, Message: "unknown stream"}
	}
	data, err := base64.StdEncoding.DecodeString(payload.Data)
	if err != nil {
		return wire.Message{}, &wire.Error{Code: wire.CodeInvalidRequest, Message: "data must be base64"}
	}
	if _, err := ch.conn.Write(data); err != nil {
		c.closeForward(stream)
		c.sendEvent(ctx, wire.TypeForwardClosed, stream, wire.ForwardClose{Error: err.Error()})
	}
	resp, rerr := wire.NewResponse(req, nil)
	if rerr != nil {
		return wire.Message{}, rerr
	}
	return resp, nil
}

// forwardClose hangs up the daemon-side connection for the stream.
func (c *connection) forwardClose(req wire.Message) (wire.Message, error) {
	if req.Stream == nil {
		return wire.Message{}, &wire.Error{Code: wire.CodeInvalidRequest, Message: "stream is required"}
	}
	c.closeForward(*req.Stream)
	resp, rerr := wire.NewResponse(req, nil)
	if rerr != nil {
		return wire.Message{}, rerr
	}
	return resp, nil
}

func (c *connection) addForward() (uint32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.forwards) >= maxForwards {
		return 0, false
	}
	c.nextStream++
	c.forwards[c.nextStream] = nil // reserve the id; filled after dial
	return c.nextStream, true
}

func (c *connection) removeForward(stream uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.forwards, stream)
}

func (c *connection) closeForward(stream uint32) {
	c.mu.Lock()
	ch, ok := c.forwards[stream]
	delete(c.forwards, stream)
	c.mu.Unlock()
	if ok && ch != nil {
		close(ch.done)
		ch.conn.Close()
	}
}

func (c *connection) closeForwards() {
	c.mu.Lock()
	streams := make([]uint32, 0, len(c.forwards))
	for s := range c.forwards {
		streams = append(streams, s)
	}
	c.mu.Unlock()
	for _, s := range streams {
		c.closeForward(s)
	}
}

// checkForwardTarget enforces the daemon's egress policy: loopback targets are
// always allowed (the daemon itself runs on a loopback listener), and any
// non-loopback target requires an explicit allowlist.
func (s *Server) checkForwardTarget(host string) error {
	if host == "" {
		return &wire.Error{Code: wire.CodeInvalidRequest, Message: "host is required"}
	}
	if len(s.cfg.ForwardAllow) > 0 {
		for _, h := range s.cfg.ForwardAllow {
			if h == "*" || h == host {
				return nil
			}
		}
		return &wire.Error{Code: wire.CodeUnsupported, Message: "forwarding to " + host + " is not allowed"}
	}
	ip := net.ParseIP(host)
	if ip == nil {
		// hostname: resolve and require every address to be loopback
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			return &wire.Error{Code: wire.CodeInvalidRequest, Message: "cannot resolve " + host}
		}
		for _, a := range addrs {
			if ip = net.ParseIP(a); ip == nil || !ip.IsLoopback() {
				return &wire.Error{Code: wire.CodeUnsupported, Message: "forwarding to " + host + " is not allowed (loopback only; start the daemon with a forward allowlist to permit other targets)"}
			}
		}
		return nil
	}
	if !ip.IsLoopback() {
		return &wire.Error{Code: wire.CodeUnsupported, Message: "forwarding to " + host + " is not allowed (loopback only; start the daemon with a forward allowlist to permit other targets)"}
	}
	return nil
}
