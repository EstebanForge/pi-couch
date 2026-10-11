// Package web serves embedded static assets. Pages render in package server.
package web

import (
	"embed"
	"io/fs"
	"net/http"
)

//go:embed static
var staticFS embed.FS

// Static serves embedded assets. no-cache keeps dev loops honest: the
// browser revalidates every load instead of heuristically caching.
func Static() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed layout is compile-time fixed
	}
	return http.StripPrefix("/static/", noCache(http.FileServer(http.FS(sub))))
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
