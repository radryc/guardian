// Package ui embeds the Forge single-page UI.
package ui

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var files embed.FS

// Handler serves the embedded UI.
func Handler() http.Handler {
	sub, err := fs.Sub(files, "static")
	if err != nil {
		panic("forge ui: " + err.Error())
	}
	return http.FileServer(http.FS(sub))
}
