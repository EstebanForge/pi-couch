// Package web serves embedded static assets. Pages render in package server.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

func Static() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed layout is compile-time fixed
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
