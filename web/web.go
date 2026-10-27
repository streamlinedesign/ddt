// Package web holds the templates and the static files the press inlines into every edition,
// and the dashboard's templates.
// Built into the binary; with -dev they are read from disk instead, so edits show without a rebuild.
package web

import (
	"embed"
	"io/fs"
	"os"
)

//go:embed templates static admin
var files embed.FS

// FS gives the web files: from the binary, or from dir when it is set.
func FS(dir string) fs.FS {
	if dir != "" {
		return os.DirFS(dir)
	}
	return files
}
