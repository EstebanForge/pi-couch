// Package web serves the embedded React SPA build.
package web

import (
	"embed"
	"io/fs"
	"net/http"
	"path"
	"strconv"
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
	".json":        "application/json",
	".png":         "image/png",
	".svg":         "image/svg+xml",
	".ico":         "image/x-icon",
}

// App serves the React SPA build (web/) at the site root. Hashed build
// assets (assets/*) cache immutably; public files (sw.js, the manifest,
// icons) are unhashed, so they revalidate on every load. Every other
// path falls back to index.html so the client router owns the URL, and
// no-store on the HTML keeps deploys honest in the WebView: a cached shell
// would pin the old immutable asset set for a year.
func App() http.Handler {
	sub, err := fs.Sub(appFS, "app")
	if err != nil {
		panic(err) // embed layout is compile-time fixed
	}
	return appHandler(sub)
}

// gzipExt marks asset types worth serving precompressed. Images and
// icons are already compressed; sidecars for them never exist.
var gzipExt = map[string]bool{
	".js":          true,
	".css":         true,
	".html":        true,
	".svg":         true,
	".json":        true,
	".webmanifest": true,
}

// AcceptsGzip reports whether the client may receive Content-Encoding:
// gzip. Every browser sends the header; only curl and the native door
// can skip it.
func AcceptsGzip(r *http.Request) bool {
	return gzipWanted(r.Header.Get("Accept-Encoding"))
}

// gzipWanted parses one Accept-Encoding header for gzip. When the
// header names gzip more than once, the last entry wins (the stricter
// read of RFC 9110's most-specific rule), so "gzip, gzip;q=0" refuses.
func gzipWanted(header string) bool {
	found, wanted := false, false
	for _, part := range strings.Split(header, ",") {
		members := strings.Split(strings.TrimSpace(part), ";")
		if strings.TrimSpace(strings.ToLower(members[0])) != "gzip" {
			continue
		}
		found = true
		q := 1.0
		for _, attr := range members[1:] {
			attr = strings.TrimSpace(strings.ToLower(attr))
			if value, ok := strings.CutPrefix(attr, "q="); ok {
				if parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil {
					q = parsed
				}
			}
		}
		wanted = q > 0
	}
	return found && wanted
}

func appHandler(sub fs.FS) http.Handler {
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
					ext := strings.ToLower(path.Ext(p))
					if ct, ok := mimeByExt[ext]; ok {
						w.Header().Set("Content-Type", ct)
					}
					if gzipExt[ext] {
						w.Header().Add("Vary", "Accept-Encoding")
						if AcceptsGzip(r) {
							if gzData, gerr := fs.ReadFile(sub, p+".gz"); gerr == nil {
								w.Header().Set("Content-Encoding", "gzip")
								w.Header().Set("Content-Length", strconv.Itoa(len(gzData)))
								_, _ = w.Write(gzData)
								return
							}
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
		w.Header().Add("Vary", "Accept-Encoding")
		if AcceptsGzip(r) {
			if gzData, gerr := fs.ReadFile(sub, "index.html.gz"); gerr == nil {
				w.Header().Set("Content-Encoding", "gzip")
				w.Header().Set("Content-Length", strconv.Itoa(len(gzData)))
				w.Header().Set("Content-Security-Policy",
					"default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
						"img-src 'self' data:; connect-src 'self'; font-src 'self'; "+
						"base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
				w.Header().Set("Content-Type", "text/html; charset=utf-8")
				w.Header().Set("Cache-Control", "no-store")
				_, _ = w.Write(gzData)
				return
			}
		}
		// The SPA is fully external (Vite emits no inline scripts) and
		// streams only from its own origin; Konsta, Virtuoso and React
		// set style attributes, so style-src keeps unsafe-inline.
		w.Header().Set("Content-Security-Policy",
			"default-src 'none'; script-src 'self'; style-src 'self' 'unsafe-inline'; "+
				"img-src 'self' data:; connect-src 'self'; font-src 'self'; "+
				"base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_, _ = w.Write(data)
	})
}
