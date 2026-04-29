// Package ui exposes the embedded SPA dist directory.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var distFS embed.FS

// FS returns the embedded dist tree (rooted at "dist/"), or an error if the
// SPA wasn't built before compiling the binary.
func FS() (fs.FS, error) {
	return fs.Sub(distFS, "dist")
}

// Available reports whether dist/index.html is present in the embedded FS.
// When false, the portal SPA isn't built yet and the HTTP layer should serve
// a placeholder instead.
func Available() bool {
	f, err := distFS.Open("dist/index.html")
	if err != nil {
		return false
	}
	_ = f.Close()
	return true
}
