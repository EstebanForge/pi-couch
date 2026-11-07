package server

import (
	"compress/gzip"
	"net/http"
	"strings"

	"github.com/EstebanForge/couch-pi/internal/web"
)

type gzipMode int

const (
	gzipNone gzipMode = iota
	gzipBuffered
	gzipFlush
)

// apiGzipMode picks compression by route. Plain JSON buffers; the
// transcript SSE flushes per frame so its large reset snapshot stays
// live. Terminal and sessions streams carry tiny frequent frames where
// flush overhead would cost more than compression saves, and images
// are already compressed.
func apiGzipMode(method, path string) gzipMode {
	if method != http.MethodGet && method != http.MethodHead {
		return gzipNone
	}
	if strings.HasPrefix(path, "/api/stream/transcript/") {
		return gzipFlush
	}
	if strings.HasPrefix(path, "/api/stream/") {
		return gzipNone
	}
	if strings.HasPrefix(path, "/api/") && !strings.HasSuffix(path, "/image") {
		return gzipBuffered
	}
	return gzipNone
}

// gzipAPI compresses API responses for clients that accept it. Wire
// order matters: guard first, then this, then the mux.
func gzipAPI(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mode := apiGzipMode(r.Method, r.URL.Path)
		if mode == gzipNone || !web.AcceptsGzip(r) {
			next.ServeHTTP(w, r)
			return
		}
		if mode == gzipFlush {
			fw := newFlushGzipWriter(w)
			next.ServeHTTP(fw, r)
			fw.finish()
			return
		}
		bw := newBufferedGzipWriter(w)
		next.ServeHTTP(bw, r)
		bw.finish()
	})
}

// gzipWriter is the shared compression core. Headers go on at
// WriteHeader because writeJSON writes the status before the body,
// and body-less codes (204, 304) stay untouched so the gzip trailer
// can never ride a body-less response.
type gzipWriter struct {
	http.ResponseWriter
	gz      *gzip.Writer
	flusher http.Flusher
	on      bool
}

func newGzipWriter(w http.ResponseWriter) gzipWriter {
	gw := gzipWriter{ResponseWriter: w}
	if f, ok := w.(http.Flusher); ok {
		gw.flusher = f
	}
	return gw
}

func bodyAllowedForGzip(code int) bool {
	return code >= http.StatusOK && code != http.StatusNoContent && code != http.StatusNotModified
}

func (w *gzipWriter) init() {
	if w.on {
		return
	}
	w.on = true
	h := w.Header()
	h.Set("Content-Encoding", "gzip")
	h.Add("Vary", "Accept-Encoding")
	h.Del("Content-Length")
	w.gz = gzip.NewWriter(w.ResponseWriter)
}

func (w *gzipWriter) WriteHeader(code int) {
	if bodyAllowedForGzip(code) {
		w.init()
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *gzipWriter) Write(b []byte) (int, error) {
	if !w.on {
		w.init()
	}
	return w.gz.Write(b)
}

func (w *gzipWriter) Unwrap() http.ResponseWriter { return w.ResponseWriter }

func (w *gzipWriter) finish() {
	if w.on {
		_ = w.gz.Close()
	}
}

// bufferedGzipWriter compresses whole JSON responses.
type bufferedGzipWriter struct {
	gzipWriter
}

func newBufferedGzipWriter(w http.ResponseWriter) *bufferedGzipWriter {
	return &bufferedGzipWriter{gzipWriter: newGzipWriter(w)}
}

// Flush flushes only the raw writer: compressed bytes deliberately
// stay inside the compressor until finish, so a mid-JSON flush cannot
// push a half-formed deflate block. A raw-only flush sends nothing new
// and is harmless.
func (w *bufferedGzipWriter) Flush() {
	if w.flusher != nil {
		w.flusher.Flush()
	}
}

// flushGzipWriter compresses an SSE stream frame by frame: the handler
// already flushes after every frame, and Flush here pushes the deflate
// block so frames reach the phone in order, compressed. The LZ77
// window survives Flush calls, so repeated names and paths still
// dedupe across frames.
type flushGzipWriter struct {
	gzipWriter
}

func newFlushGzipWriter(w http.ResponseWriter) *flushGzipWriter {
	return &flushGzipWriter{gzipWriter: newGzipWriter(w)}
}

func (w *flushGzipWriter) Flush() {
	if w.on {
		_ = w.gz.Flush()
	}
	if w.flusher != nil {
		w.flusher.Flush()
	}
}
