// Package server wires the HTTP surface: the React SPA, JSON feeds and
// commands, and the terminal mirror stream.
package server

import (
	"context"
	"encoding/binary"
	"errors"
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
	"github.com/EstebanForge/couch-pi/internal/push"
	"github.com/EstebanForge/couch-pi/internal/store"
	"github.com/EstebanForge/couch-pi/internal/web"
)

const defaultTail = 40

// heartbeat re-renders badges even when no session file moves.
const heartbeat = 30 * time.Second

// terminalPoll is the pane.read tick. herdr 0.9.3 has no output push.
const terminalPoll = time.Second

// changesPoll is the working-tree scan tick (settled R4 throttle).
const changesPoll = 3 * time.Second

type app struct {
	logger        *slog.Logger
	idx           *pi.Index
	herdr         *herdr.Client
	watcher       *pi.Watcher // nil: heartbeat-only updates
	st            *store.Store
	pusher        *push.Sender
	vapidPublic   string
	allowedOrigin string // -origin; the public https origin served through tailscale
}

func Run(ctx context.Context, logger *slog.Logger, addr string, idx *pi.Index, hc *herdr.Client, w *pi.Watcher, st *store.Store, pusher *push.Sender, allowedOrigin string) error {
	a := &app{logger: logger, idx: idx, herdr: hc, watcher: w, st: st, pusher: pusher, allowedOrigin: allowedOrigin}
	if pusher != nil {
		a.vapidPublic = pusher.Public
	}
	srv := &http.Server{
		Addr:              addr,
		Handler:           a.guard(a.routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("couch-pi listening", "addr", addr)
	srvErr := make(chan error, 1)
	go func() {
		err := srv.ListenAndServe()
		if !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()
	select {
	case err := <-srvErr:
		return err
	case <-ctx.Done():
	}
	// Graceful stop: drain briefly, then force-close (SSE streams never idle).
	shutCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutCtx); err != nil {
		return srv.Close()
	}
	return nil
}

// guard is the cross-site defense for state-changing requests. The tailnet
// is the auth boundary (no login layer by decision), but a hostile page open
// in the phone's browser must not drive terminals cross-site. Browsers send
// Sec-Fetch-Site on every request; curl and the native door send no headers
// and pass. The Host allowlist stops DNS rebinding against the loopback bind.
func (a *app) guard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// JSON reads are data exfiltration targets for a rebound host: a
		// desk-browser page resolving the loopback name to attacker DNS can
		// read transcripts otherwise. Page GETs stay host-open; the loopback
		// bind and the tailnet remain the boundary.
		sensitive := r.Method != http.MethodGet && r.Method != http.MethodHead
		if !sensitive {
			sensitive = strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/stream/")
		}
		if sensitive {
			switch r.Header.Get("Sec-Fetch-Site") {
			case "", "same-origin", "none":
			default:
				a.logger.Warn("blocked cross-site request", "fetch-site", r.Header.Get("Sec-Fetch-Site"), "path", r.URL.Path)
				http.Error(w, "cross-site request", http.StatusForbidden)
				return
			}
			if o := r.Header.Get("Origin"); o != "" && !a.originAllowed(o, r.Host) {
				a.logger.Warn("blocked foreign origin", "origin", o, "path", r.URL.Path)
				http.Error(w, "cross-origin request", http.StatusForbidden)
				return
			}
			host := hostOnly(r.Host)
			if !localHost(host) && (a.allowedOrigin == "" || host != originHost(a.allowedOrigin)) {
				a.logger.Warn("blocked rebound host", "host", r.Host, "path", r.URL.Path)
				http.Error(w, "unknown host", http.StatusForbidden)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (a *app) originAllowed(origin, reqHost string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	if reqHost != "" && u.Host == reqHost {
		return true
	}
	return localHost(u.Hostname()) || (a.allowedOrigin != "" && u.Hostname() == originHost(a.allowedOrigin))
}

func localHost(host string) bool {
	return host == "localhost" || host == "127.0.0.1" || host == "::1"
}

// hostOnly strips the port from a Host header or origin (IPv6 aware).
func hostOnly(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return host
	}
	return hostport
}

func originHost(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return u.Hostname()
}

func (a *app) routes() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = w.Write([]byte("ok\n"))
	})

	// The React SPA is the whole site; the templ pages died at cutover.
	mux.Handle("GET /", web.App())
	mux.HandleFunc("GET /api/sessions", a.apiSessions)
	mux.HandleFunc("GET /api/stream/sessions", a.apiStreamSessions)
	mux.HandleFunc("GET /api/stream/transcript/{key}", a.apiStreamTranscript)
	mux.HandleFunc("GET /api/stream/changes/{key}", a.apiStreamChanges)
	mux.HandleFunc("GET /api/sessions/{key}/changes", a.apiChanges)
	mux.HandleFunc("GET /api/sessions/{key}/files", a.apiFiles)
	mux.HandleFunc("GET /api/sessions/{key}/files/view", a.apiFilesView)
	mux.HandleFunc("GET /api/sessions/{key}/comments", a.apiComments)
	mux.HandleFunc("POST /api/sessions/{key}/comments", a.apiSendAll)
	mux.HandleFunc("POST /api/sessions/{key}/stop", a.apiStop)
	mux.HandleFunc("POST /api/sessions/{key}/close", a.apiClose)
	mux.HandleFunc("POST /api/sessions/{key}/prompt", a.apiPrompt)
	mux.HandleFunc("POST /api/sessions/{key}/model", a.apiModel)
	mux.HandleFunc("POST /api/sessions/{key}/thinking", a.apiThinking)
	mux.HandleFunc("POST /api/sessions/{key}/terminal/input", a.terminalInput)
	// The terminal mirror is a raw-byte SSE stream with custom event
	// names (term/gone/ping), not one of the JSON /api/stream feeds.
	mux.HandleFunc("GET /stream/terminal/{key}", a.streamTerminal)
	mux.HandleFunc("GET /api/models", a.models)
	mux.HandleFunc("POST /api/upload", a.uploadHandler)
	mux.HandleFunc("GET /api/push/key", a.pushKey)
	mux.HandleFunc("POST /api/push/subscribe", a.pushSubscribe)
	mux.HandleFunc("POST /api/push/unsubscribe", a.pushUnsubscribe)
	return mux
}

// sessionsWire is the JSON snapshot the React SPA boots from (rewrite-plan.md).
// Nil slices and maps marshal as null, so both fields are always non-nil.
type sessionsWire struct {
	Sessions []pi.SessionMeta  `json:"sessions"`
	Badges   map[string]string `json:"badges"`
}

func (a *app) apiSessions(w http.ResponseWriter, r *http.Request) {
	sessions, badges := a.snapshot(r)
	if sessions == nil {
		sessions = []pi.SessionMeta{}
	}
	if badges == nil {
		badges = map[string]string{}
	}
	writeJSON(w, http.StatusOK, sessionsWire{Sessions: sessions, Badges: badges})
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

// promptAgent drives the JSON prompt endpoint: submit the text to the
// agent steering this session, or a typed error.
func (a *app) promptAgent(key, text string) error {
	text = strings.TrimSpace(text)
	if text == "" {
		return errEmptyText
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		return errNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		return herdr.ErrNotDriven
	}
	if err := a.herdr.Prompt(ctx, target, text); err != nil {
		a.logger.Error("prompt", "err", err)
		return errPromptFail
	}
	return nil
}

func itemFps(items []pi.ViewItem) []uint32 {
	out := make([]uint32, len(items))
	for i := range items {
		it := &items[i]
		h := fnv.New32a()
		h.Write([]byte(it.Kind))
		h.Write([]byte{0})
		h.Write([]byte(it.Text))
		h.Write([]byte{0})
		h.Write([]byte(it.ToolName))
		var b [11]byte
		if it.IsError {
			b[0] = 1
		}
		if it.Redacted {
			b[1] = 1
		}
		binary.LittleEndian.PutUint64(b[2:], uint64(it.DurationMs))
		b[10] = byte(it.Images)
		h.Write(b[:])
		out[i] = h.Sum32()
	}
	return out
}
func chatAppend(prev []uint32, items []pi.ViewItem, cur []uint32) (delta []pi.ViewItem, ok bool) {
	if len(prev) == 0 || len(items) < len(prev) || len(cur) != len(items) {
		return nil, false
	}
	for i, f := range prev {
		if cur[i] != f {
			return nil, false
		}
	}
	return items[len(prev):], true
}

// subscribeTicks registers one fan-out slot; nil watcher degrades to
// heartbeat-only updates.
func (a *app) subscribeTicks() (<-chan struct{}, func()) {
	if a.watcher == nil {
		return nil, func() {}
	}
	return a.watcher.Subscribe()
}

// streamMinInterval spaces a stream's full-tail re-renders. Active
// sessions emit watcher ticks per token batch, and patching per tick
// flooded clients with back-to-back DOM replacements until they froze.
const streamMinInterval = time.Second

// keepaliveEvery re-sends the tiny badge patch on an otherwise idle
// stream so proxies and tailscale see traffic between content changes.
const keepaliveEvery = 15 * time.Second

// pacer rate-limits one stream's patch loop.
type pacer struct {
	last time.Time
}

// wait blocks until the next patch is due: immediately on the first
// call, then at most once per streamMinInterval (coalescing the tick
// firehose), or on the heartbeat when nothing changes. False means the
// client left.
func (p *pacer) wait(ctx context.Context, ticks <-chan struct{}) bool {
	if p.last.IsZero() {
		p.last = time.Now()
		return true
	}
	if d := streamMinInterval - time.Since(p.last); d > 0 {
		select {
		case <-ctx.Done():
			return false
		case <-time.After(d):
		}
	}
	select {
	case <-ticks:
		p.last = time.Now()
		return true
	case <-time.After(heartbeat):
		p.last = time.Now()
		return true
	case <-ctx.Done():
		return false
	}
}
