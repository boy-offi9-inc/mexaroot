package wire

type HelloRequest struct {
	Client   string `json:"client"`
	Protocol []int  `json:"protocol"`
	Token    string `json:"token"`
}

type HelloResponse struct {
	Daemon       string   `json:"daemon"`
	Protocol     int      `json:"protocol"`
	Capabilities []string `json:"capabilities"`
}

type RuntimeInfo struct {
	ID    string `json:"id"`
	Kind  string `json:"kind"`
	Name  string `json:"name"`
	State string `json:"state"`
}

type RuntimeListResponse struct {
	Runtimes []RuntimeInfo `json:"runtimes"`
}

type ExecRequest struct {
	Runtime   string            `json:"runtime"`
	Argv      []string          `json:"argv"`
	Cwd       string            `json:"cwd,omitempty"`
	Env       map[string]string `json:"env,omitempty"`
	TimeoutMS int64             `json:"timeout_ms,omitempty"`
}

type ExecResponse struct {
	ExitCode int    `json:"exit_code"`
	Stdout   string `json:"stdout"`
	Stderr   string `json:"stderr"`
	// Truncated is true when output hit the daemon's exec cap (8 MiB per
	// stream); everything past the cap was discarded, not buffered.
	Truncated bool `json:"truncated,omitempty"`
}

type SessionStartRequest struct {
	Runtime string            `json:"runtime"`
	Shell   string            `json:"shell,omitempty"`
	Cols    int               `json:"cols"`
	Rows    int               `json:"rows"`
	Env     map[string]string `json:"env,omitempty"`
}

// SessionListResponse lists the daemon's resumable sessions.
type SessionListResponse struct {
	Sessions []SessionInfo `json:"sessions"`
}

// SessionInfo describes one resumable session.
type SessionInfo struct {
	Stream   uint32 `json:"stream"`
	Runtime  string `json:"runtime"`
	Shell    string `json:"shell,omitempty"`
	Owned    bool   `json:"owned"`    // owned by the asking connection
	Detached bool   `json:"detached"` // no connection owns it right now
	Buffered int    `json:"buffered"` // replay bytes waiting
}

// SessionAttachRequest re-attaches to a session that survived a
// disconnect. Output produced while detached is replayed first.
type SessionAttachRequest struct {
	Stream uint32 `json:"stream"`
	Cols   int    `json:"cols,omitempty"`
	Rows   int    `json:"rows,omitempty"`
}

type StreamResponse struct {
	Stream uint32 `json:"stream"`
}

type SessionInput struct {
	Stream uint32 `json:"stream"`
	Data   string `json:"data"`
}

type SessionResize struct {
	Stream uint32 `json:"stream"`
	Cols   int    `json:"cols"`
	Rows   int    `json:"rows"`
}

type SessionClose struct {
	Stream uint32 `json:"stream"`
}

type SessionOutput struct {
	Data string `json:"data"`
}

type SessionExit struct {
	ExitCode int `json:"exit_code"`
}

type FilePushRequest struct {
	Runtime string `json:"runtime"`
	Path    string `json:"path"`
	Size    int64  `json:"size"`
	SHA256  string `json:"sha256"`
	Mode    uint32 `json:"mode,omitempty"`
}

type FilePullRequest struct {
	Runtime string `json:"runtime"`
	Path    string `json:"path"`
}

type FilePullResponse struct {
	Stream uint32 `json:"stream"`
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
	Mode   uint32 `json:"mode"`
}

type FileDone struct {
	OK    bool   `json:"ok"`
	Error *Error `json:"error,omitempty"`
}

type ForwardOpenRequest struct {
	// Host is the destination the daemon dials. Must be loopback unless the
	// daemon was started with an allowlist of permitted hosts.
	Host string `json:"host"`
	Port int    `json:"port"`
}

type ForwardOpenResponse struct {
	Stream uint32 `json:"stream"`
}

// ForwardClose is sent by either side to half-close/hang up a channel.
type ForwardClose struct {
	Stream uint32 `json:"stream"`
	// Error describes why the channel ended, when it did not close cleanly.
	Error string `json:"error,omitempty"`
}

// ForwardFailure is the open rejection event pushed back to the client.
type ForwardFailure struct {
	Error string `json:"error"`
}
