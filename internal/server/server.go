// Package server wires the HTTP surface: templ pages, static assets,
// Datastar SSE streams, and (next slice) steering.
package server

import (
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/EstebanForge/piremote/internal/herdr"
	"github.com/EstebanForge/piremote/internal/pi"
	"github.com/EstebanForge/piremote/internal/web"
	"github.com/EstebanForge/piremote/internal/web/pages"
)

const defaultTail = 40

// heartbeat re-renders badges even when no session file moves.
const heartbeat = 30 * time.Second

type app struct {
	logger  *slog.Logger
	idx     *pi.Index
	herdr   *herdr.Client
	watcher *pi.Watcher // nil: heartbeat-only updates
}

func Run(logger *slog.Logger, addr string, idx *pi.Index, hc *herdr.Client, w *pi.Watcher) error {
	a := &app{logger: logger, idx: idx, herdr: hc, watcher: w}
	srv := &http.Server{
		Addr:              addr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("piremote listening", "addr", addr)
	err := srv.ListenAndServe()
	if errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return err
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	mux.HandleFunc("GET /", a.list)
	mux.HandleFunc("GET /stream/sessions", a.streamSessions)
	mux.HandleFunc("GET /sessions/{key}", a.transcript)
	mux.Handle("GET /static/", web.Static())
	return mux
}

func (a *app) list(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path != "/" {
		http.NotFound(w, r)
		return
	}
	sessions, badges := a.snapshot(r)
	serveTempl(w, r, pages.List(sessions, badges))
}

func (a *app) streamSessions(w http.ResponseWriter, r *http.Request) {
	sse := datastar.NewSSE(w, r)
	for {
		sessions, badges := a.snapshot(r)
		err := sse.PatchElementTempl(
			pages.SessionList(sessions, badges),
			datastar.WithSelector("#session-list"),
			datastar.WithModeInner(),
		)
		if err != nil {
			return // client gone or write failed; close the stream
		}
		select {
		case <-r.Context().Done():
			return
		default:
		}
		if a.watcher != nil {
			select {
			case <-a.watcher.Events():
			case <-time.After(heartbeat):
			case <-r.Context().Done():
				return
			}
		} else {
			select {
			case <-time.After(heartbeat):
			case <-r.Context().Done():
				return
			}
		}
	}
}

func (a *app) snapshot(r *http.Request) ([]pi.SessionMeta, map[string]string) {
	sessions, err := a.idx.List()
	if err != nil {
		a.logger.Error("session list", "err", err)
		sessions = nil
	}
	badges := a.herdr.Badges(r.Context())
	return sessions, badges
}

func (a *app) transcript(w http.ResponseWriter, r *http.Request) {
	limit := defaultTail
	if n, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && n > 0 && n <= 1000 {
		limit = n
	}
	meta, items, err := a.idx.Transcript(r.PathValue("key"), limit)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	serveTempl(w, r, pages.Transcript(meta, items))
}

func serveTempl(w http.ResponseWriter, r *http.Request, c templ.Component) {
	_ = c.Render(r.Context(), w) // headers may already be gone on error
}
