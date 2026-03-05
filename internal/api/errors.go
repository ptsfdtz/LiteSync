package api

import (
	"errors"
	"fmt"
)

var (
	ErrInvalidArgument  = errors.New("invalid argument")
	ErrJobNotFound      = errors.New("job not found")
	ErrAlreadyRunning   = errors.New("already running")
	ErrPermissionDenied = errors.New("permission denied")
	ErrIOTransient      = errors.New("io transient error")
	ErrConflictDetected = errors.New("conflict detected")
	ErrNotSupported     = errors.New("not supported")
	ErrInternal         = errors.New("internal error")
	ErrNotImplemented   = errors.New("not implemented")
)

func Wrap(kind error, msg string) error {
	if kind == nil {
		return errors.New(msg)
	}
	return fmt.Errorf("%w: %s", kind, msg)
}

func ErrorCode(err error) string {
	switch {
	case err == nil:
		return "OK"
	case errors.Is(err, ErrInvalidArgument):
		return "INVALID_ARGUMENT"
	case errors.Is(err, ErrJobNotFound):
		return "JOB_NOT_FOUND"
	case errors.Is(err, ErrAlreadyRunning):
		return "ALREADY_RUNNING"
	case errors.Is(err, ErrPermissionDenied):
		return "PERMISSION_DENIED"
	case errors.Is(err, ErrIOTransient):
		return "IO_TRANSIENT"
	case errors.Is(err, ErrConflictDetected):
		return "CONFLICT_DETECTED"
	case errors.Is(err, ErrNotSupported):
		return "NOT_SUPPORTED"
	case errors.Is(err, ErrNotImplemented):
		return "NOT_IMPLEMENTED"
	default:
		return "INTERNAL"
	}
}
