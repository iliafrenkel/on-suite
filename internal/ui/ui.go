// Package ui holds the embedded static files and HTML templates for the whole
// suite. It is a leaf package: it contains no logic, only the embed
// directives, so that anything can depend on it without acquiring
// dependencies of its own.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed static
var staticFiles embed.FS

//go:embed templates
var templateFiles embed.FS

// Static is the tree served under /static/.
func Static() fs.FS { return must(fs.Sub(staticFiles, "static")) }

// Templates is the shared layout and platform pages. Apps supply their own
// templates separately.
func Templates() fs.FS { return must(fs.Sub(templateFiles, "templates")) }

func must(fsys fs.FS, err error) fs.FS {
	if err != nil {
		// Unreachable: the paths are compile-time constants verified by go:embed.
		panic("ui: embedded files missing: " + err.Error())
	}
	return fsys
}
