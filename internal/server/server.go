// Package server wires the HTTP surface: templ pages, static assets,
// Datastar SSE streams, and (next slice) steering.
package server

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/a-h/templ"
	"github.com/starfederation/datastar-go/datastar"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
	"github.com/EstebanForge/couch-pi/internal/store"
	"github.com/EstebanForge/couch-pi/internal/web"
	"github.com/EstebanForge/couch-pi/internal/web/pages"
)

const defaultTail = 40

// heartbeat re-renders badges even when no session file moves.
const heartbeat = 30 * time.Second

// terminalPoll is the pane.read tick. herdr 0.9.3 has no output push.
const terminalPoll = time.Second

// changesPoll is the working-tree scan tick (settled R4 throttle).
const changesPoll = 3 * time.Second

type app struct {
	logger  *slog.Logger
	idx     *pi.Index
	herdr   *herdr.Client
	watcher *pi.Watcher // nil: heartbeat-only updates
	st      *store.Store
}

func Run(logger *slog.Logger, addr string, idx *pi.Index, hc *herdr.Client, w *pi.Watcher, st *store.Store) error {
	a := &app{logger: logger, idx: idx, herdr: hc, watcher: w, st: st}
	srv := &http.Server{
		Addr:              addr,
		Handler:           a.routes(),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("couch-pi listening", "addr", addr)
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
	mux.HandleFunc("GET /stream/transcript/{key}", a.streamTranscript)
	mux.HandleFunc("GET /sessions/{key}", a.transcript)
	mux.HandleFunc("GET /sessions/{key}/changes", a.changes)
	mux.HandleFunc("GET /stream/changes/{key}", a.streamChanges)
	mux.HandleFunc("GET /sessions/{key}/files", a.files)
	mux.HandleFunc("GET /sessions/{key}/files/view", a.filesView)
	mux.HandleFunc("GET /sessions/{key}/comments", a.comments)
	mux.HandleFunc("POST /sessions/{key}/comments", a.sendAll)
	mux.HandleFunc("GET /sessions/{key}/terminal", a.terminal)
	mux.HandleFunc("GET /stream/terminal/{key}", a.streamTerminal)
	mux.HandleFunc("POST /sessions/{key}/terminal/input", a.terminalInput)
	mux.HandleFunc("POST /sessions/{key}/prompt", a.steerPrompt)
	mux.HandleFunc("POST /sessions/{key}/stop", a.steerStop)
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
		if !a.waitTick(r) {
			return
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
	badges := a.herdr.Badges(r.Context())
	serveTempl(w, r, pages.Transcript(meta, items, badges[meta.Path], noticeText(r.URL.Query())))
}

// noticeText maps POST redirect flags into one line of feedback.
func noticeText(q url.Values) string {
	switch {
	case q.Get("sent") == "1":
		return "Sent. Steering queues while the agent works."
	case q.Get("stopped") == "1":
		return "Ctrl-C sent."
	case q.Get("err") == "notdriven":
		return "No herdr driver is running this session."
	case q.Get("err") == "empty":
		return "Nothing to send."
	default:
		return ""
	}
}

func (a *app) steerPrompt(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	back := "/sessions/" + url.PathEscape(key)
	text := strings.TrimSpace(r.FormValue("text"))
	if text == "" {
		http.Redirect(w, r, back+"?err=empty", http.StatusSeeOther)
		return
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		http.Redirect(w, r, back+"?err=notdriven", http.StatusSeeOther)
		return
	}
	if err := a.herdr.Prompt(ctx, target, text); err != nil {
		a.logger.Error("prompt", "err", err)
		http.Redirect(w, r, back+"?err=notdriven", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"?sent=1", http.StatusSeeOther)
}

func (a *app) steerStop(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	back := "/sessions/" + url.PathEscape(key)
	meta, ok := a.idx.Meta(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		http.Redirect(w, r, back+"?err=notdriven", http.StatusSeeOther)
		return
	}
	if err := a.herdr.SendKeys(ctx, target, []string{"ctrl+c"}); err != nil {
		a.logger.Error("stop", "err", err)
	}
	http.Redirect(w, r, back+"?stopped=1", http.StatusSeeOther)
}

// streamTranscript patches the chat tail, driver badge, and stop box on
// session writes and heartbeat.
func (a *app) streamTranscript(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	limit := defaultTail
	if n, err := strconv.Atoi(r.URL.Query().Get("n")); err == nil && n > 0 && n <= 1000 {
		limit = n
	}
	sse := datastar.NewSSE(w, r)
	for {
		meta, ok := a.idx.Meta(key)
		if !ok {
			return
		}
		_, items, err := a.idx.Transcript(key, limit)
		if err != nil {
			return
		}
		badges := a.herdr.Badges(r.Context())
		status := badges[meta.Path]
		for _, patch := range []struct {
			sel string
			c   templ.Component
		}{
			{"#chat", pages.TranscriptItems(items)},
			{"#driver-badge", pages.DriverBadge(status)},
			{"#stop-box", pages.StopForm(key, status)},
		} {
			if err := sse.PatchElementTempl(patch.c, datastar.WithSelector(patch.sel), datastar.WithModeInner()); err != nil {
				return
			}
		}
		if !a.waitTick(r) {
			return
		}
	}
}

// waitTick blocks for the next update trigger; false means the client left.
func (a *app) waitTick(r *http.Request) bool {
	ctx := r.Context()
	if a.watcher != nil {
		select {
		case <-a.watcher.Events():
			return true
		case <-time.After(heartbeat):
			return true
		case <-ctx.Done():
			return false
		}
	}
	select {
	case <-time.After(heartbeat):
		return true
	case <-ctx.Done():
		return false
	}
}

func serveTempl(w http.ResponseWriter, r *http.Request, c templ.Component) {
	_ = c.Render(r.Context(), w) // headers may already be gone on error
}
