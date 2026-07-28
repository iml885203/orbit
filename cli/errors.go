package cli

import (
	"errors"
	"fmt"
	"path/filepath"
)

// Sentinel kinds for CLI error classification. Wrap them via the New*Error
// constructors so WriteJSONError maps failures to stable machine codes.
var (
	ErrUnknownResource    = errors.New("unknown resource")
	ErrTimeout            = errors.New("timeout")
	ErrNotConfigured      = errors.New("not configured")
	ErrChecksFailed       = errors.New("checks failed")
	ErrDependencyBlocked  = errors.New("dependency blocked")
	ErrServiceStartFailed = errors.New("service start failed")
	ErrLogsUnavailable    = errors.New("logs unavailable")
	ErrEnvRepoAccess      = errors.New("environment repository access failed")
	ErrEnvRepoUnavailable = errors.New("environment repository unavailable")
	ErrInitIncomplete     = errors.New("initialization incomplete")
	ErrInvalidEnvironment = errors.New("invalid environment")
	ErrInvalidArgument    = errors.New("invalid argument")
)

type ResourcePortConflictError struct {
	Port           int
	Resource       string
	PID            string
	Process        string
	InspectCommand string
}

func (e *ResourcePortConflictError) Error() string {
	owner := ""
	if e.PID != "" && e.PID != "?" {
		owner = " by pid " + e.PID
		if e.Process != "" && e.Process != "?" {
			owner = " by " + filepath.Base(e.Process) + " (pid " + e.PID + ")"
		}
	}
	return fmt.Sprintf("cannot start %s: port %d is already in use%s", e.Resource, e.Port, owner)
}

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

func NewUnknownResourceError(name string) error {
	return classifiedError{kind: ErrUnknownResource, msg: "unknown resource: " + name}
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

func NewLogsUnavailableError(msg string) error {
	return classifiedError{kind: ErrLogsUnavailable, msg: msg}
}

func NewEnvRepoAccessError(msg string) error {
	return classifiedError{kind: ErrEnvRepoAccess, msg: msg}
}

func NewEnvRepoUnavailableError(msg string) error {
	return classifiedError{kind: ErrEnvRepoUnavailable, msg: msg}
}

func NewInitIncompleteError(msg string) error {
	return classifiedError{kind: ErrInitIncomplete, msg: msg}
}

func NewInvalidEnvironmentError(msg string) error {
	return classifiedError{kind: ErrInvalidEnvironment, msg: msg}
}

func NewInvalidArgumentError(msg string) error {
	return classifiedError{kind: ErrInvalidArgument, msg: msg}
}
