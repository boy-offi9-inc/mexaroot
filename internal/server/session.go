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
	session, stream, err := c.startSession(ctx, req)
	if err != nil {
		c.send(ctx, errorResponse(req, err))
		return
	}

	resp, err := wire.NewResponse(req, wire.StreamResponse{Stream: stream})
	if err != nil {
		c.removeSession(stream)
		session.Close()
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

	stream, ok := c.addSession(session)
	if !ok {
		session.Close()
		return nil, 0, &wire.Error{Code: wire.CodeInvalidRequest, Message: "too many open sessions"}
	}
	return session, stream, nil
}

func (c *connection) pump(ctx context.Context, stream uint32, session runtimes.Session) {
	defer c.wg.Done()

	buf := make([]byte, outputChunk)
	for {
		n, err := session.Read(buf)
		if n > 0 {
			c.sendEvent(ctx, wire.TypeSessionOutput, stream, wire.SessionOutput{
				Data: base64.StdEncoding.EncodeToString(buf[:n]),
			})
		}
		if err != nil {
			break
		}
	}

	code, err := session.Wait()
	if err != nil {
		code = -1
	}
	c.removeSession(stream)
	session.Close()
	c.sendEvent(ctx, wire.TypeSessionExit, stream, wire.SessionExit{ExitCode: code})
}

func (c *connection) sessionInput(req wire.Message) (wire.Message, error) {
	var params wire.SessionInput
	if err := req.DecodePayload(&params); err != nil {
		return wire.Message{}, err
	}
	session, err := c.lookupSession(params.Stream)
	if err != nil {
		return wire.Message{}, err
	}
	data, err := base64.StdEncoding.DecodeString(params.Data)
	if err != nil {
		return wire.Message{}, &wire.Error{Code: wire.CodeInvalidRequest, Message: "data must be base64"}
	}
	if _, err := session.Write(data); err != nil {
		return wire.Message{}, err
	}
	return wire.NewResponse(req, nil)
}

func (c *connection) sessionResize(req wire.Message) (wire.Message, error) {
	var params wire.SessionResize
	if err := req.DecodePayload(&params); err != nil {
		return wire.Message{}, err
	}
	session, err := c.lookupSession(params.Stream)
	if err != nil {
		return wire.Message{}, err
	}
	if err := session.Resize(params.Cols, params.Rows); err != nil {
		return wire.Message{}, err
	}
	return wire.NewResponse(req, nil)
}

func (c *connection) sessionClose(req wire.Message) (wire.Message, error) {
	var params wire.SessionClose
	if err := req.DecodePayload(&params); err != nil {
		return wire.Message{}, err
	}
	session, err := c.lookupSession(params.Stream)
	if err != nil {
		return wire.Message{}, err
	}
	session.Close()
	return wire.NewResponse(req, nil)
}

func (c *connection) lookupSession(stream uint32) (runtimes.Session, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	session, ok := c.sessions[stream]
	if !ok {
		return nil, &wire.Error{Code: wire.CodeNotFound, Message: "unknown stream"}
	}
	return session, nil
}

func (c *connection) addSession(session runtimes.Session) (uint32, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.sessions) >= maxSessions {
		return 0, false
	}
	c.nextStream++
	c.sessions[c.nextStream] = session
	return c.nextStream, true
}

func (c *connection) removeSession(stream uint32) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.sessions, stream)
}

func (c *connection) closeSessions() {
	c.mu.Lock()
	open := make([]runtimes.Session, 0, len(c.sessions))
	for _, session := range c.sessions {
		open = append(open, session)
	}
	c.mu.Unlock()

	for _, session := range open {
		session.Close()
	}
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
