// Package web serves the embedded React SPA build.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"strings"
)

//go:embed all:app
var appFS embed.FS

// mimeByExt pins content types the Go mime table misses (the manifest
// must not sniff to text/plain).
var mimeByExt = map[string]string{
	".webmanifest": "application/manifest+json",
	".js":          "text/javascript; charset=utf-8",
	".css":         "text/css; charset=utf-8",
	".html":        "text/html; charset=utf-8",
	".png":         "image/png",
	".svg":         "image/svg+xml",
	".ico":         "image/x-icon",
}

// App serves the React SPA build (web/) at the site root. Hashed build
// assets (assets/*) cache immutably; public files (sw.js, the manifest,
// icons) are unhashed, so they revalidate on every load. Every other
// path falls back to index.html so the client router owns the URL, and
// no-cache on the HTML keeps deploys honest in the WebView.
func App() http.Handler {
	sub, err := fs.Sub(appFS, "app")
	if err != nil {
		panic(err) // embed layout is compile-time fixed
	}
	files := http.FileServerFS(sub)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := strings.Trim(r.URL.Path, "/")
		if p != "" {
			d, oerr := sub.Open(p)
			if oerr == nil {
				info, serr := d.Stat()
				d.Close()
				switch {
				case serr != nil:
					http.NotFound(w, r)
					return
				case info.IsDir():
					http.NotFound(w, r) // no listings; the asset set is fixed
					return
				default:
					if strings.HasPrefix(p, "assets/") {
						w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
					} else {
						w.Header().Set("Cache-Control", "no-cache")
					}
					if ext := p[strings.LastIndex(p, "."):]; ext != p && ext != "" {
						if ct, ok := mimeByExt[strings.ToLower(ext)]; ok {
							w.Header().Set("Content-Type", ct)
						}
					}
					files.ServeHTTP(w, r)
					return
				}
			}
			if strings.Contains(p, ".") {
				http.NotFound(w, r) // asset-shaped path that is not in the build
				return
			}
		}
		data, rerr := fs.ReadFile(sub, "index.html")
		if rerr != nil {
			http.NotFound(w, r) // fresh clone without a web build
			return
		}
		// The SPA is fully external (Vite emits no inline scripts) and
		// streams only from its own origin; Konsta, Virtuoso and React
		// set style attributes, so style-src keeps unsafe-inline.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; font-src 'self'; "+
				"base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-cache")
		_, _ = w.Write(data)
	})
}
