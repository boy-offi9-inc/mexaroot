package server

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"slices"
	"sync"
	"time"

	"github.com/coder/websocket"

	"github.com/boy-offi9-inc/mexaroot/internal/auth"
	"github.com/boy-offi9-inc/mexaroot/internal/runtimes"
	"github.com/boy-offi9-inc/mexaroot/internal/wire"
)

const (
	maxMessageSize  = 1 << 20
	maxInflight     = 32
	writeTimeout    = 10 * time.Second
	shutdownTimeout = 5 * time.Second
)

var (
	ErrNoToken     = errors.New("token is required")
	ErrNoRegistry  = errors.New("runtime registry is required")
	ErrNonLoopback = errors.New("listen address is not a loopback address")
)

type Config struct {
	Addr             string
	Token            string
	Version          string
	Registry         *runtimes.Registry
	AllowNonLoopback bool
}

type Server struct {
	cfg          Config
	capabilities []string
	limiter      *handshakeLimiter
}

func New(cfg Config) (*Server, error) {
	if cfg.Token == "" {
		return nil, ErrNoToken
	}
	if cfg.Registry == nil {
		return nil, ErrNoRegistry
	}
	if !cfg.AllowNonLoopback {
		if err := requireLoopback(cfg.Addr); err != nil {
			return nil, err
		}
	}
	return &Server{
		cfg: cfg,
		capabilities: []string{
			wire.CapabilityExec,
			wire.CapabilitySession,
			wire.CapabilityFile,
			wire.CapabilityKeepalive,
		},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	return mux
}

func (s *Server) ListenAndServe(ctx context.Context) error {
	httpServer := &http.Server{
		Addr:              s.cfg.Addr,
		Handler:           s.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		BaseContext:       func(net.Listener) context.Context { return ctx },
	}

	errc := make(chan error, 1)
	go func() { errc <- httpServer.ListenAndServe() }()

	select {
	case err := <-errc:
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		return httpServer.Shutdown(shutdownCtx)
	}
}

func (s *Server) has(capability string) bool {
	return slices.Contains(s.capabilities, capability)
}

func (s *Server) handleWebSocket(w http.ResponseWriter, r *http.Request) {
	if s.limiter.blocked(r.RemoteAddr) {
		http.Error(w, "too many failed handshakes; try again later", http.StatusTooManyRequests)
		return
	}
	conn, err := websocket.Accept(w, r, nil)
	if err != nil {
		return
	}
	defer conn.CloseNow()
	conn.SetReadLimit(maxMessageSize)

	c := &connection{
		server:    s,
		remoteIP:  r.RemoteAddr,
		conn:      conn,
		sessions:  make(map[uint32]runtimes.Session),
		transfers: make(map[uint32]*fileTransfer),
	}
	c.serve(r.Context())
}

func requireLoopback(addr string) error {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("invalid listen address %q: %w", addr, err)
	}
	if host == "localhost" {
		return nil
	}
	ip := net.ParseIP(host)
	if ip == nil || !ip.IsLoopback() {
		return fmt.Errorf("%w: %s", ErrNonLoopback, addr)
	}
	return nil
}

type connection struct {
	server   *Server
	remoteIP string
	conn     *websocket.Conn
	wg       sync.WaitGroup

	mu         sync.Mutex
	sessions   map[uint32]runtimes.Session
	transfers  map[uint32]*fileTransfer
	nextStream uint32
}

func (c *connection) serve(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		c.closeSessions()
		c.closeTransfers()
		c.wg.Wait()
	}()

	if !c.handshake(ctx) {
		return
	}

	inflight := make(chan struct{}, maxInflight)
	for {
		msgType, data, err := c.conn.Read(ctx)
		if err != nil {
			return
		}
		if msgType == websocket.MessageBinary {
			c.routeChunk(ctx, data)
			continue
		}
		if msgType != websocket.MessageText {
			c.conn.Close(websocket.StatusUnsupportedData, "binary frames are not supported")
			return
		}
		msg, err := wire.Decode(data)
		if err != nil {
			c.conn.Close(websocket.StatusInvalidFramePayloadData, "malformed message")
			return
		}
		if !runsConcurrently(msg.Type) {
			c.respond(ctx, msg)
			continue
		}
		inflight <- struct{}{}
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			defer func() { <-inflight }()
			c.respond(ctx, msg)
		}()
	}
}

func (c *connection) routeChunk(ctx context.Context, data []byte) {
	stream, chunk, err := wire.DecodeChunk(data)
	if err != nil {
		return
	}
	c.mu.Lock()
	tr, ok := c.transfers[stream]
	c.mu.Unlock()
	if !ok {
		return
	}
	select {
	case tr.chunks <- chunk:
	case <-tr.done:
	}
}

func runsConcurrently(msgType string) bool {
	return msgType == wire.TypeExecRun
}

func (c *connection) handshake(ctx context.Context) bool {
	msgType, data, err := c.conn.Read(ctx)
	if err != nil {
		return false
	}
	if msgType != websocket.MessageText {
		c.server.limiter.recordFailure(c.remoteIP)
		c.conn.Close(websocket.StatusUnsupportedData, "binary frames are not supported")
		return false
	}
	msg, err := wire.Decode(data)
	if err != nil {
		c.server.limiter.recordFailure(c.remoteIP)
		c.conn.Close(websocket.StatusInvalidFramePayloadData, "malformed message")
		return false
	}
	if msg.Type != wire.TypeHello {
		c.server.limiter.recordFailure(c.remoteIP)
		c.reject(ctx, msg, wire.CodeUnauthorized, "handshake required")
		return false
	}

	var req wire.HelloRequest
	if err := msg.DecodePayload(&req); err != nil {
		c.server.limiter.recordFailure(c.remoteIP)
		c.send(ctx, errorResponse(msg, err))
		c.conn.Close(websocket.StatusPolicyViolation, "invalid handshake")
		return false
	}
	if !auth.Equal(req.Token, c.server.cfg.Token) {
		c.server.limiter.recordFailure(c.remoteIP)
		c.reject(ctx, msg, wire.CodeUnauthorized, "invalid token")
		return false
	}
	version, ok := wire.Negotiate(req.Protocol, wire.SupportedVersions)
	if !ok {
		text := fmt.Sprintf("no common protocol version; supported: %v", wire.SupportedVersions)
		c.reject(ctx, msg, wire.CodeUnsupported, text)
		return false
	}

	resp, err := wire.NewResponse(msg, wire.HelloResponse{
		Daemon:       c.server.cfg.Version,
		Protocol:     version,
		Capabilities: c.server.capabilities,
	})
	if err != nil {
		c.conn.Close(websocket.StatusInternalError, "internal error")
		return false
	}
	c.server.limiter.success(c.remoteIP)
	c.send(ctx, resp)
	return true
}

func (c *connection) reject(ctx context.Context, req wire.Message, code, text string) {
	c.send(ctx, wire.NewErrorResponse(req, code, text))
	c.conn.Close(websocket.StatusPolicyViolation, code)
}

func (c *connection) send(ctx context.Context, msg wire.Message) {
	data, err := wire.Encode(msg)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, writeTimeout)
	defer cancel()
	c.conn.Write(ctx, websocket.MessageText, data)
}

func (c *connection) respond(ctx context.Context, msg wire.Message) {
	capability, known := wire.RequiredCapability(msg.Type)
	if !known || (capability != "" && !c.server.has(capability)) {
		c.send(ctx, wire.NewErrorResponse(msg, wire.CodeUnsupported, "unsupported message type: "+msg.Type))
		return
	}

	var (
		resp wire.Message
		err  error
	)
	switch msg.Type {
	case wire.TypeRuntimeList:
		resp, err = c.runtimeList(msg)
	case wire.TypeExecRun:
		resp, err = c.execRun(ctx, msg)
	case wire.TypePing:
		resp, err = wire.NewResponse(msg, nil)
	case wire.TypeSessionStart:
		c.handleSessionStart(ctx, msg)
		return
	case wire.TypeSessionInput:
		resp, err = c.sessionInput(msg)
	case wire.TypeSessionResize:
		resp, err = c.sessionResize(msg)
	case wire.TypeSessionClose:
		resp, err = c.sessionClose(msg)
	case wire.TypeFilePush:
		c.handleFilePush(ctx, msg)
		return
	case wire.TypeFilePull:
		c.handleFilePull(ctx, msg)
		return
	default:
		resp = wire.NewErrorResponse(msg, wire.CodeInvalidRequest, "unexpected message type: "+msg.Type)
	}
	if err != nil {
		resp = errorResponse(msg, err)
	}
	c.send(ctx, resp)
}

func (c *connection) runtimeList(req wire.Message) (wire.Message, error) {
	infos := c.server.cfg.Registry.List()
	list := make([]wire.RuntimeInfo, 0, len(infos))
	for _, info := range infos {
		list = append(list, wire.RuntimeInfo{
			ID:    info.ID,
			Kind:  string(info.Kind),
			Name:  info.Name,
			State: string(info.State),
		})
	}
	return wire.NewResponse(req, wire.RuntimeListResponse{Runtimes: list})
}

func (c *connection) execRun(ctx context.Context, req wire.Message) (wire.Message, error) {
	var params wire.ExecRequest
	if err := req.DecodePayload(&params); err != nil {
		return wire.Message{}, err
	}
	if params.Runtime == "" || len(params.Argv) == 0 {
		return wire.Message{}, &wire.Error{Code: wire.CodeInvalidRequest, Message: "runtime and argv are required"}
	}

	rt, err := c.server.cfg.Registry.Get(params.Runtime)
	if err != nil {
		return wire.Message{}, &wire.Error{Code: wire.CodeNotFound, Message: err.Error()}
	}

	result, err := rt.Exec(ctx, runtimes.ExecRequest{
		Argv:    params.Argv,
		Cwd:     params.Cwd,
		Env:     params.Env,
		Timeout: time.Duration(params.TimeoutMS) * time.Millisecond,
	})
	switch {
	case errors.Is(err, runtimes.ErrTimeout):
		return wire.Message{}, &wire.Error{Code: wire.CodeTimeout, Message: err.Error()}
	case errors.Is(err, runtimes.ErrEmptyCommand), errors.Is(err, runtimes.ErrInvalidArgument):
		return wire.Message{}, &wire.Error{Code: wire.CodeInvalidRequest, Message: err.Error()}
	case err != nil:
		return wire.Message{}, err
	}

	return wire.NewResponse(req, wire.ExecResponse{
		ExitCode:  result.ExitCode,
		Stdout:    string(result.Stdout),
		Stderr:    string(result.Stderr),
		Truncated: result.Truncated,
	})
}

func errorResponse(req wire.Message, err error) wire.Message {
	var werr *wire.Error
	if errors.As(err, &werr) {
		return wire.NewErrorResponse(req, werr.Code, werr.Message)
	}
	return wire.NewErrorResponse(req, wire.CodeInternal, err.Error())
}
