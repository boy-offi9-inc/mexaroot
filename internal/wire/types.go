package wire

const (
	TypeHello         = "hello"
	TypeRuntimeList   = "runtime.list"
	TypeExecRun       = "exec.run"
	TypeSessionStart  = "session.start"
	TypeSessionInput  = "session.input"
	TypeSessionResize = "session.resize"
	TypeSessionClose  = "session.close"
	TypeSessionOutput = "session.output"
	TypeSessionExit   = "session.exit"
	TypeFilePush      = "file.push"
	TypeFilePull      = "file.pull"
	TypeFileDone      = "file.done"
	TypePing          = "ping"
)

const (
	CapabilityExec      = "exec"
	CapabilitySession   = "session"
	CapabilityFile      = "file"
	CapabilityKeepalive = "keepalive"
)

func RequiredCapability(msgType string) (string, bool) {
	switch msgType {
	case TypeHello, TypeRuntimeList:
		return "", true
	case TypeExecRun:
		return CapabilityExec, true
	case TypeSessionStart, TypeSessionInput, TypeSessionResize, TypeSessionClose:
		return CapabilitySession, true
	case TypeFilePush, TypeFilePull:
		return CapabilityFile, true
	case TypePing:
		return CapabilityKeepalive, true
	default:
		return "", false
	}
}
