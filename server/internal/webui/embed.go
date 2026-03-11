package webui

import (
	"embed"
	"io/fs"
)

// DistFiles embeds frontend build artifacts copied into internal/webui/dist.
//
//go:embed all:dist
var DistFiles embed.FS

func DistFS() (fs.FS, error) {
	return fs.Sub(DistFiles, "dist")
}

