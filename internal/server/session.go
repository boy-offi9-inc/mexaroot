package server

import (
	"context"
	"encoding/base64"
	"errors"

	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const (
	outputChunk = 32 * 1024
	maxSessions = 16
	defaultTerm = "xterm-256color"
)

func (c *connection) handleSessionStart(ctx context.Context, req wire.Message) {
	var params wire.SessionStartRequest
	_ = req.DecodePayload(&params) // startSession re-decodes and validates
	session, stream, err := c.startSession(ctx, req)
	if err != nil {
		c.server.audit.write(AuditEntry{
			Event:    "session_start",
			RemoteIP: hostOf(c.remoteIP),
			Runtime:  params.Runtime,
			Error:    err.Error(),
		})
		c.send(ctx, errorResponse(req, err))
		return
	}
	c.server.audit.write(AuditEntry{
		Event:    "session_start",
		RemoteIP: hostOf(c.remoteIP),
		Runtime:  params.Runtime,
	})

	resp, err := wire.NewResponse(req, wire.StreamResponse{Stream: stream})
	if err != nil {
		c.removeSession(stream)
		c.send(ctx, errorResponse(req, err))
		return
	}
	c.send(ctx, resp)

	c.wg.Add(1)
	go c.pump(ctx, stream, session)
}

func (c *connection) startSession(ctx context.Context, req wire.Message) (runtimes.Session, uint32, error) {
	var params wire.SessionStartRequest
	if err := req.DecodePayload(&params); err != nil {
		return nil, 0, err
	}
	if params.Runtime == "" {
		return nil, 0, &wire.Error{Code: wire.CodeInvalidRequest, Message: "runtime is required"}
	}

	rt, err := c.server.cfg.Registry.Get(params.Runtime)
	if err != nil {
		return nil, 0, &wire.Error{Code: wire.CodeNotFound, Message: err.Error()}
	}
	interactive, ok := rt.(runtimes.Interactive)
	if !ok {
		return nil, 0, &wire.Error{Code: wire.CodeUnsupported, Message: "runtime does not support interactive sessions"}
	}

	var argv []string
	if params.Shell != "" {
		argv = []string{params.Shell}
	}
	session, err := interactive.StartSession(ctx, runtimes.SessionRequest{
		Argv: argv,
		Env:  sessionEnv(params.Env),
		Cols: params.Cols,
		Rows: params.Rows,
	})
	if err != nil {
		if errors.Is(err, runtimes.ErrInvalidArgument) {
			return nil, 0, &wire.Error{Code: wire.CodeInvalidRequest, Message: err.Error()}
		}
		return nil, 0, err
	}

	stream, ok := c.addSession(session, params.Runtime, params.Shell)
	if !ok {
		session.Close()
		return nil, 0, &wire.Error{Code: wire.CodeInvalidRequest, Message: "too many open sessions"}
	}
	return session, stream, nil
}

// addSession registers a new session in the daemon-wide registry. The
// stream number is unique across connections, so a reattaching client can
// address it after a reconnect.
func (c *connection) addSession(session runtimes.Session, runtime, shell string) (uint32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.server.resume.count() >= maxSessions {
		return 0, false
	}
	stream := c.server.resume.allocID()
	c.server.resume.add(stream, session, runtime, shell, c)
	return stream, true
}

func (c *connection) removeSession(stream uint32) {
	if ds := c.server.resume.remove(stream); ds != nil {
		ds.session.Close()
	}
}

// pump reads session output until the process exits. While the session is
// owned by a connection, output is forwarded as events to that owner
// (which may change across reattach); while detached, it accumulates in
// the replay buffer. The pump runs under the server's background context
// so it survives its starting connection disconnecting.
func (c *connection) pump(ctx context.Context, stream uint32, session runtimes.Session) {
	defer c.wg.Done()
	ds := c.server.resume.get(stream)
	if ds == nil {
		return
	}
	// Outlive the starting connection: events route to the current owner
	// through ds, not through this connection's request context.
	pctx, pcancel := context.WithCancel(context.Background())
	defer pcancel()

	buf := make([]byte, outputChunk)
	for {
		n, err := session.Read(buf)
		if n > 0 {
			ds.push(buf[:n])
			if owner := ds.currentOwner(); owner != nil {
				owner.sendEvent(pctx, wire.TypeSessionOutput, stream, wire.SessionOutput{
					Data: base64.StdEncoding.EncodeToString(buf[:n]),
				})
			}
		}
		if err != nil {
			break
		}
	}

	code, err := session.Wait()
	if err != nil {
		code = -1
	}
	c.server.resume.remove(stream)
	session.Close()
	if owner := ds.currentOwner(); owner != nil {
		owner.sendEvent(pctx, wire.TypeSessionExit, stream, wire.SessionExit{ExitCode: code})
	}
}

func (c *connection) sessionList(req wire.Message) (wire.Message, error) {
	return wire.NewResponse(req, wire.SessionListResponse{
		Sessions: resumeListToWire(c.server.resume.list(c)),
	})
}

func resumeListToWire(infos []SessionInfo) []wire.SessionInfo {
	out := make([]wire.SessionInfo, 0, len(infos))
	for _, in := range infos {
		out = append(out, wire.SessionInfo{
			Stream:   in.Stream,
			Runtime:  in.Runtime,
			Shell:    in.Shell,
			Owned:    in.Owned,
			Detached: in.Detached,
			Buffered: in.Buffered,
		})
	}
	return out
}

// handleSessionAttach re-attaches to a resumable session, replays output
// buffered while the session was detached, then continues the normal
// output pump.
func (c *connection) handleSessionAttach(ctx context.Context, req wire.Message) {
	var params wire.SessionAttachRequest
	if err := req.DecodePayload(&params); err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}
	ds, ok := c.server.resume.attach(params.Stream, c)
	if ds == nil {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeNotFound, Message: "unknown session"}))
		return
	}
	if !ok {
		c.send(ctx, errorResponse(req, &wire.Error{Code: wire.CodeInvalidRequest, Message: "session is attached to another connection"}))
		return
	}

	c.server.audit.write(AuditEntry{
		Event:    "session_attach",
		RemoteIP: hostOf(c.remoteIP),
		Runtime:  ds.runtime,
	})

	if params.Cols > 0 && params.Rows > 0 {
		_ = ds.session.Resize(params.Cols, params.Rows)
	}

	resp, err := wire.NewResponse(req, wire.StreamResponse{Stream: params.Stream})
	if err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}
	c.send(ctx, resp)

	// Replay what accumulated while detached, then live output flows.
	if replay := ds.drain(); len(replay) > 0 {
		c.sendEvent(ctx, wire.TypeSessionOutput, params.Stream, wire.SessionOutput{
			Data: base64.StdEncoding.EncodeToString(replay),
		})
	}
}

func (c *connection) sessionInput(req wire.Message) (wire.Message, error) {
	var params wire.SessionInput
	if err := req.DecodePayload(&params); err != nil {
		return wire.Message{}, err
	}
	ds := c.server.resume.get(params.Stream)
	if ds == nil {
		return wire.Message{}, &wire.Error{Code: wire.CodeNotFound, Message: "unknown stream"}
	}
	data, err := base64.StdEncoding.DecodeString(params.Data)
	if err != nil {
		return wire.Message{}, &wire.Error{Code: wire.CodeInvalidRequest, Message: "data must be base64"}
	}
	if _, err := ds.session.Write(data); err != nil {
		return wire.Message{}, err
	}
	return wire.NewResponse(req, nil)
}

func (c *connection) sessionResize(req wire.Message) (wire.Message, error) {
	var params wire.SessionResize
	if err := req.DecodePayload(&params); err != nil {
		return wire.Message{}, err
	}
	ds := c.server.resume.get(params.Stream)
	if ds == nil {
		return wire.Message{}, &wire.Error{Code: wire.CodeNotFound, Message: "unknown stream"}
	}
	if err := ds.session.Resize(params.Cols, params.Rows); err != nil {
		return wire.Message{}, err
	}
	return wire.NewResponse(req, nil)
}

func (c *connection) sessionClose(req wire.Message) (wire.Message, error) {
	var params wire.SessionClose
	if err := req.DecodePayload(&params); err != nil {
		return wire.Message{}, err
	}
	c.removeSession(params.Stream)
	return wire.NewResponse(req, nil)
}

func (c *connection) sendEvent(ctx context.Context, msgType string, stream uint32, payload any) {
	ev, err := wire.NewEvent(msgType, stream, payload)
	if err != nil {
		return
	}
	c.send(ctx, ev)
}

func sessionEnv(env map[string]string) map[string]string {
	merged := make(map[string]string, len(env)+1)
	for key, value := range env {
		merged[key] = value
	}
	if _, ok := merged["TERM"]; !ok {
		merged["TERM"] = defaultTerm
	}
	return merged
}
