//go:build !windows

package folderpicker

func pick(_ string) (string, error) {
	return "", ErrNotSupported
}
