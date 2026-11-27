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
	// AuditPath enables the audit log (JSON Lines, mode 0600) when non-empty.
	// Off by default. Records connections, auth failures, exec commands and
	// session starts; see audit.go.
	AuditPath string
	// ForwardAllow lists hostnames or IPs the forward capability may dial.
	// "*" permits any target. Empty restricts forwarding to loopback.
	ForwardAllow []string
	// Devices holds per-device tokens (optional). When set, a hello may
	// authenticate with either the shared Token or any live device token;
	// the matched device name lands in the audit trail. Token may then be
	// empty, disabling the shared token entirely.
	Devices *auth.Store
	// DetachGrace is how long a session survives without a connection
	// after its owner disconnects. Zero uses DefaultDetachGrace.
	DetachGrace time.Duration
}

type Server struct {
	cfg          Config
	capabilities []string
	limiter      *handshakeLimiter
	audit        *auditLog
	resume       *sessionRegistry
	pair         *pairManager
}

func New(cfg Config) (*Server, error) {
	if cfg.Token == "" && cfg.Devices == nil {
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
	audit, err := newAuditLog(cfg.AuditPath)
	if err != nil {
		return nil, err
	}
	registry := newSessionRegistry(cfg.DetachGrace)
	registry.start()
	return &Server{
		cfg:     cfg,
		pair:    newPairManager(),
		limiter: newHandshakeLimiter(),
		audit:   audit,
		resume:  registry,
		capabilities: []string{
			wire.CapabilityExec,
			wire.CapabilitySession,
			wire.CapabilityFile,
			wire.CapabilityForward,
			wire.CapabilityKeepalive,
		},
	}, nil
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/ws", s.handleWebSocket)
	mux.HandleFunc("/pair", s.handlePair)
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
	if s.audit != nil && s.audit.f != nil {
		s.audit.write(AuditEntry{Event: "connect", RemoteIP: hostOf(r.RemoteAddr)})
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
		forwards:  make(map[uint32]*forwardChannel),
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

	// deviceCaps is nil for shared-token connections (full access) and
	// non-nil for per-device tokens, where it is the capability whitelist.
	deviceCaps []string

	mu         sync.Mutex
	sessions   map[uint32]runtimes.Session
	transfers  map[uint32]*fileTransfer
	forwards   map[uint32]*forwardChannel
	nextStream uint32
}

func (c *connection) serve(ctx context.Context) {
	ctx, cancel := context.WithCancel(ctx)
	defer func() {
		cancel()
		c.closeTransfers()
		c.closeForwards()
		c.server.resume.detach(c)
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
		c.server.audit.write(AuditEntry{Event: "auth_failed", RemoteIP: hostOf(c.remoteIP)})
		c.conn.Close(websocket.StatusUnsupportedData, "binary frames are not supported")
		return false
	}
	msg, err := wire.Decode(data)
	if err != nil {
		c.server.limiter.recordFailure(c.remoteIP)
		c.server.audit.write(AuditEntry{Event: "auth_failed", RemoteIP: hostOf(c.remoteIP)})
		c.conn.Close(websocket.StatusInvalidFramePayloadData, "malformed message")
		return false
	}
	if msg.Type != wire.TypeHello {
		c.server.limiter.recordFailure(c.remoteIP)
		c.server.audit.write(AuditEntry{Event: "auth_failed", RemoteIP: hostOf(c.remoteIP)})
		c.reject(ctx, msg, wire.CodeUnauthorized, "handshake required")
		return false
	}

	var req wire.HelloRequest
	if err := msg.DecodePayload(&req); err != nil {
		c.server.limiter.recordFailure(c.remoteIP)
		c.server.audit.write(AuditEntry{Event: "auth_failed", RemoteIP: hostOf(c.remoteIP)})
		c.send(ctx, errorResponse(msg, err))
		c.conn.Close(websocket.StatusPolicyViolation, "invalid handshake")
		return false
	}
	device, deviceOK := auth.Device{}, false
	if c.server.cfg.Devices != nil {
		device, deviceOK = c.server.cfg.Devices.Verify(req.Token)
	}
	if !deviceOK && !auth.Equal(req.Token, c.server.cfg.Token) {
		c.server.limiter.recordFailure(c.remoteIP)
		c.server.audit.write(AuditEntry{Event: "auth_failed", RemoteIP: hostOf(c.remoteIP)})
		c.reject(ctx, msg, wire.CodeUnauthorized, "invalid token")
		return false
	}
	if deviceOK {
		c.deviceCaps = device.Capabilities
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
		Capabilities: c.effectiveCapabilities(),
	})
	if err != nil {
		c.conn.Close(websocket.StatusInternalError, "internal error")
		return false
	}
	c.server.limiter.success(c.remoteIP)
	if deviceOK {
		c.server.audit.write(AuditEntry{Event: "device_auth", RemoteIP: hostOf(c.remoteIP), Detail: device.Name})
	}
	c.send(ctx, resp)
	return true
}

// deviceCapabilityFor maps a message type to the per-device capability it
// needs. ok=false means the type is always allowed (listing, ping).
func deviceCapabilityFor(msgType string) (string, bool) {
	switch msgType {
	case wire.TypeExecRun:
		return auth.CapExec, true
	case wire.TypeSessionStart, wire.TypeSessionAttach, wire.TypeSessionInput, wire.TypeSessionResize, wire.TypeSessionClose:
		return auth.CapShell, true
	case wire.TypeFilePush:
		return auth.CapFilesWrite, true
	case wire.TypeFilePull:
		return auth.CapFilesRead, true
	case wire.TypeForwardOpen, wire.TypeForwardData, wire.TypeForwardClose:
		return auth.CapForward, true
	}
	return "", false
}

// effectiveCapabilities is what the hello advertises: the daemon's own
// capability list, narrowed by a per-device whitelist when the connection
// authenticated with a device token.
func (c *connection) effectiveCapabilities() []string {
	if c.deviceCaps == nil {
		return c.server.capabilities
	}
	var out []string
	for _, cap := range c.server.capabilities {
		// The device vocabulary is finer than the protocol's: grant the
		// coarse protocol capability if any device capability maps to it.
		if deviceGrantedForProtocol(cap, c.deviceCaps) {
			out = append(out, cap)
		}
	}
	return out
}

// deviceGrantedForProtocol maps a coarse protocol capability (exec, session,
// file, forward, keepalive) onto the per-device whitelist.
func deviceGrantedForProtocol(protocolCap string, caps []string) bool {
	switch protocolCap {
	case wire.CapabilityExec:
		return auth.HasCapability(caps, auth.CapExec)
	case wire.CapabilitySession:
		return auth.HasCapability(caps, auth.CapShell)
	case wire.CapabilityFile:
		return auth.HasCapability(caps, auth.CapFilesRead) || auth.HasCapability(caps, auth.CapFilesWrite)
	case wire.CapabilityForward:
		return auth.HasCapability(caps, auth.CapForward)
	case wire.CapabilityKeepalive:
		return true
	}
	return false
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
	// Per-device whitelist: finer-grained than the protocol capabilities,
	// so file push/pull can be separated and spectator devices can list
	// but not type.
	if cap, needed := deviceCapabilityFor(msg.Type); c.deviceCaps != nil && needed && !auth.HasCapability(c.deviceCaps, cap) {
		c.send(ctx, wire.NewErrorResponse(msg, wire.CodeForbidden, "device token lacks the "+cap+" capability"))
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
	case wire.TypeSessionList:
		resp, err = c.sessionList(msg)
	case wire.TypeSessionAttach:
		c.handleSessionAttach(ctx, msg)
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
	case wire.TypeForwardOpen:
		c.handleForwardOpen(ctx, msg)
		return
	case wire.TypeForwardData:
		resp, err = c.forwardData(ctx, msg)
	case wire.TypeForwardClose:
		resp, err = c.forwardClose(msg)
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

	c.server.audit.write(AuditEntry{
		Event:    "exec",
		RemoteIP: hostOf(c.remoteIP),
		Runtime:  params.Runtime,
		Argv:     params.Argv,
	})

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
