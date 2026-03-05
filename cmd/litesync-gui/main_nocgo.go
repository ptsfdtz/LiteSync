//go:build !cgo

package main

import (
	"fmt"
	"os"
)

func main() {
	_, _ = fmt.Fprintln(os.Stderr, "LiteSync GUI 需要 CGO_ENABLED=1。请先执行: set CGO_ENABLED=1 ，然后运行 go run ./cmd/litesync-gui")
	os.Exit(1)
}
