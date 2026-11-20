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
