package cli

import "errors"

// Sentinel kinds for CLI error classification. Wrap them via the New*Error
// constructors so WriteJSONError maps failures to stable machine codes.
var (
	ErrUnknownService     = errors.New("unknown service")
	ErrTimeout            = errors.New("timeout")
	ErrNotConfigured      = errors.New("not configured")
	ErrChecksFailed       = errors.New("checks failed")
	ErrDependencyBlocked  = errors.New("dependency blocked")
	ErrServiceStartFailed = errors.New("service start failed")
	ErrEnvRepoAccess      = errors.New("environment repository access failed")
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

func NewChecksFailedError(msg string) error {
	return classifiedError{kind: ErrChecksFailed, msg: msg}
}

func NewDependencyBlockedError(msg string) error {
	return classifiedError{kind: ErrDependencyBlocked, msg: msg}
}

func NewServiceStartFailedError(msg string) error {
	return classifiedError{kind: ErrServiceStartFailed, msg: msg}
}

func NewEnvRepoAccessError(msg string) error {
	return classifiedError{kind: ErrEnvRepoAccess, msg: msg}
}
