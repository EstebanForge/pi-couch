// Package web holds templ pages and embedded static assets.
package web

import (
	"embed"
	"io/fs"
	"net/http"

	"github.com/EstebanForge/piremote/internal/web/pages"
	"github.com/a-h/templ"
)

//go:embed static
var staticFS embed.FS

func Home() http.Handler {
	return templ.Handler(pages.Home())
}

func Static() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed layout is compile-time fixed
	}
	return http.StripPrefix("/static/", http.FileServer(http.FS(sub)))
}
