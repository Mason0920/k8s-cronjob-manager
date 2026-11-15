// Package web embeds the web UI built by Vite into the Go binary.
//
// `npm run build` writes the UI to dist/ui (ignored by git). A fresh clone
// only has dist/placeholder, so `go build ./...` works without Node and the
// binary serves a short page explaining how to build the UI.
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var dist embed.FS

// UI returns the built UI and true, or the placeholder page and false when
// the UI has not been built.
func UI() (fs.FS, bool) {
	if sub, err := fs.Sub(dist, "dist/ui"); err == nil {
		if _, err := fs.Stat(sub, "index.html"); err == nil {
			return sub, true
		}
	}
	sub, err := fs.Sub(dist, "dist/placeholder")
	if err != nil {
		panic(err) // dist/placeholder is committed; this cannot happen
	}
	return sub, false
}
