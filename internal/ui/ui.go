// Package ui embeds the built web UI (spec 11). The frontend lives in web/
// and is built into dist/ by `make web`; dist/ is not committed, so a plain
// `go build` serves a page that says the UI is missing.
package ui

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// FS is the built UI.
func FS() fs.FS {
	sub, _ := fs.Sub(dist, "dist")
	return sub
}

// Built reports whether dist/ holds a built UI.
func Built() bool {
	_, err := fs.Stat(FS(), "index.html")
	return err == nil
}
