// Package server wires the HTTP surface: templ pages, static assets, and
// (from the SSE slice on) Datastar endpoints.
package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"

	"github.com/EstebanForge/piremote/internal/pi"
	"github.com/EstebanForge/piremote/internal/web"
	"github.com/EstebanForge/piremote/internal/web/pages"
)

const defaultTail = 40

func Run(logger *slog.Logger, addr string, idx *pi.Index) error {
	srv := &http.Server{
		Addr:              addr,
		Handler:           routes(logger, idx),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("piremote listening", "addr", addr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func routes(logger *slog.Logger, idx *pi.Index) http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		sessions, err := idx.List()
		if err != nil {
			http.Error(w, "sessions unavailable", http.StatusServiceUnavailable)
			return
		}
		serveTempl(w, r, pages.List(sessions))
	})

	mux.HandleFunc("GET /sessions/{key}", func(w http.ResponseWriter, r *http.Request) {
		limit := defaultTail
		if n, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && n > 0 && n <= 1000 {
			limit = n
		}
		meta, items, err := idx.Transcript(r.PathValue("key"), limit)
		if err != nil {
			http.NotFound(w, r)
			return
		}
		_ = logger
		serveTempl(w, r, pages.Transcript(meta, items))
	})

	mux.Handle("GET /static/", web.Static())
	return mux
}

func serveTempl(w http.ResponseWriter, r *http.Request, c templ.Component) {
	err := c.Render(r.Context(), w)
	if err != nil {
		// Headers may already be gone; nothing sane left to send.
		_ = err
	}
}
