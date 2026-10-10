// Package server wires the HTTP surface: templ pages, static assets, and
// (from M1 on) Datastar SSE endpoints.
package server

import (
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/EstebanForge/piremote/internal/web"
)

func Run(logger *slog.Logger, addr string) error {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.Handle("GET /", web.Home())
	mux.Handle("GET /static/", web.Static())

	srv := &http.Server{
		Addr:              addr,
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	logger.Info("piremote listening", "addr", addr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}
