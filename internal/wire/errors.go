package wire

const (
	CodeUnauthorized   = "unauthorized"
	CodeForbidden      = "forbidden"
	CodeUnsupported    = "unsupported"
	CodeInvalidRequest = "invalid_request"
	CodeNotFound       = "not_found"
	CodeTimeout        = "timeout"
	CodeInternal       = "internal"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return e.Code + ": " + e.Message
}
