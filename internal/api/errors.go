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
