package hostexec

import "errors"

var ErrPTYUnsupported = errors.New("pseudo-terminals are not supported on this platform")
