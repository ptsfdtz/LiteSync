package folderpicker

import "errors"

var (
	ErrNotSupported = errors.New("folder picker is not supported on this platform")
	ErrCancelled    = errors.New("folder selection was cancelled")
)

func Pick(initialPath string) (string, error) {
	return pick(initialPath)
}
