package fastsocket

import "errors"

var (
	ErrSessionNil             = errors.New("session is nil")
	ErrHandlerNil             = errors.New("handler is nil")
	ErrSessionManagerNotFound = errors.New("not found session")
)
