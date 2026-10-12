// Package web serves embedded static assets. Pages render in package server.
package web

import (
	"crypto/sha256"
	"embed"
	"encoding/hex"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed static
var staticFS embed.FS

// assetVer holds a short content hash per embedded asset. It versions asset
// URLs in page heads so a deploy busts client caches (the WebView included).
var assetVer = map[string]string{}

func init() {
	for _, p := range []string{"app.css", "js/main.js"} {
		b, err := staticFS.ReadFile("static/" + p)
		if err != nil {
			panic(err) // embed layout is compile-time fixed
		}
		sum := sha256.Sum256(b)
		assetVer[p] = hex.EncodeToString(sum[:4])
	}
}

// AssetPath versions a build-artifact asset URL (/static/app.css,
// /static/js/main.js); vendored assets pass through untouched.
func AssetPath(p string) string {
	if v, ok := assetVer[strings.TrimPrefix(p, "/static/")]; ok {
		return p + "?v=" + v
	}
	return p
}

// Static serves embedded assets. no-cache keeps dev loops honest: the
// browser revalidates every load instead of heuristically caching.
// Directories 404: no listings, the asset set is fixed.
func Static() http.Handler {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err) // embed layout is compile-time fixed
	}
	files := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// StripPrefix leaves "" for /static/ itself; no listings either way.
		if r.URL.Path == "" || strings.HasSuffix(r.URL.Path, "/") {
			http.NotFound(w, r)
			return
		}
		http.FileServerFS(sub).ServeHTTP(w, r)
	})
	return http.StripPrefix("/static/", noCache(files))
}

// ServiceWorker returns the root-scoped push service worker source. Push
// needs the worker at /sw.js so its scope covers every route.
func ServiceWorker() ([]byte, error) {
	return staticFS.ReadFile("static/js/sw.js")
}

// Manifest returns the PWA manifest.
func Manifest() ([]byte, error) {
	return staticFS.ReadFile("static/manifest.webmanifest")
}

func noCache(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-cache")
		next.ServeHTTP(w, r)
	})
}
