package cli

import "errors"

// Sentinel kinds for CLI error classification. Wrap them via the New*Error
// constructors so WriteJSONError maps failures to stable machine codes.
var (
	ErrUnknownService = errors.New("unknown service")
	ErrTimeout        = errors.New("timeout")
	ErrNotConfigured  = errors.New("not configured")
)

type classifiedError struct {
	kind error
	msg  string
}

func (e classifiedError) Error() string {
	return e.msg
}

func (e classifiedError) Unwrap() error {
	return e.kind
}

func NewUnknownServiceError(name string) error {
	return classifiedError{kind: ErrUnknownService, msg: "unknown service: " + name}
}

func NewTimeoutError(msg string) error {
	return classifiedError{kind: ErrTimeout, msg: msg}
}

func NewNotConfiguredError(msg string) error {
	return classifiedError{kind: ErrNotConfigured, msg: msg}
}
