// Package server wires the HTTP surface: the React SPA, JSON feeds and
// commands, and the terminal mirror stream.
package server

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"hash/fnv"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EstebanForge/couch-pi/internal/config"
	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
	"github.com/EstebanForge/couch-pi/internal/push"
	"github.com/EstebanForge/couch-pi/internal/quotas"
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
	allowedOrigin string // config origin; the public https origin served through tailscale
	uploadDir     string // config uploads_dir; where POST /api/upload lands
	home          string // server home; the client tildifies paths against it
	awaiting      *awaitingDetector
	bridgeMu      sync.Mutex
	bridge        map[string]bridgeEntry // session path -> open pi dialog (bridge extension)
	answered      *dialogMemos           // per-dialog answer claims; dedupes double taps
	dialogMu      sync.Mutex
	dialogLatch   map[string]*dialogWire // last good wire parse per session; a flaked tick must not drop the card's body
	models        *modelsCache           // pi catalog: memory, state file, then pi
	memory        *memoryProbe           // agent memory server health
	dialogWake    chan struct{}          // cap 1: a dialog changed; stream loops push now
	quotas        *quotas.Service        // provider quotas & limits tracker
}

func Run(ctx context.Context, logger *slog.Logger, cfg *config.Config, idx *pi.Index, hc *herdr.Client, w *pi.Watcher, st *store.Store, pusher *push.Sender) error {
	home, _ := os.UserHomeDir()
	a := &app{logger: logger, idx: idx, herdr: hc, watcher: w, st: st, pusher: pusher, allowedOrigin: cfg.Origin, uploadDir: cfg.UploadsDir, home: home, answered: newDialogMemos(), models: newModelsCache(filepath.Join(st.Dir(), "models.json")), memory: newMemoryProbe(cfg.MemoryURL, cfg.MemorySecret), dialogWake: make(chan struct{}, 1), dialogLatch: map[string]*dialogWire{}, quotas: quotas.NewService()}
	if pusher != nil {
		a.vapidPublic = pusher.Public
	}
	// Dialog awareness: poll working panes for pi-tui chrome and expose
	// per-session "waiting for input" state on the wire.
	a.awaiting = newAwaitingDetector(hc, time.Second)
	a.awaiting.onChange = a.wakeDialog
	go a.awaiting.run(ctx)
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.guard(a.routes()),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("couch-pi listening", "addr", cfg.Addr)
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
	mux.HandleFunc("GET /api/version", a.apiVersion)

	// The React SPA is the whole site; the templ pages died at cutover.
	mux.Handle("GET /", web.App())
	mux.HandleFunc("GET /api/sessions", a.apiSessions)
	mux.HandleFunc("GET /api/stream/sessions", a.apiStreamSessions)
	mux.HandleFunc("GET /api/stream/transcript/{key}", a.apiStreamTranscript)
	mux.HandleFunc("GET /api/stream/changes/{key}", a.apiStreamChanges)
	mux.HandleFunc("GET /api/sessions/{key}/changes", a.apiChanges)
	mux.HandleFunc("GET /api/sessions/{key}/items/{seq}", a.apiItem)
	mux.HandleFunc("GET /api/sessions/{key}/items/{seq}/image", a.apiItemImage)
	mux.HandleFunc("GET /api/sessions/{key}/transcript", a.apiTranscriptPage)
	mux.HandleFunc("GET /api/sessions/{key}/files", a.apiFiles)
	mux.HandleFunc("GET /api/sessions/{key}/files/view", a.apiFilesView)
	mux.HandleFunc("GET /api/sessions/{key}/comments", a.apiComments)
	mux.HandleFunc("POST /api/sessions/{key}/comments", a.apiSendAll)
	mux.HandleFunc("POST /api/sessions/{key}/stop", a.apiStop)
	mux.HandleFunc("POST /api/sessions/{key}/close", a.apiClose)
	mux.HandleFunc("POST /api/sessions/{key}/prompt", a.apiPrompt)
	mux.HandleFunc("POST /api/sessions/{key}/dialog/answer", a.apiDialogAnswer)
	mux.HandleFunc("POST /api/sessions/{key}/model", a.apiModel)
	mux.HandleFunc("POST /api/sessions/{key}/thinking", a.apiThinking)
	mux.HandleFunc("POST /api/bridge/dialog", a.apiBridgeDialog)
	mux.HandleFunc("GET /api/dirs", a.apiDirs)
	mux.HandleFunc("GET /api/workspaces", a.apiWorkspaces)
	mux.HandleFunc("POST /api/agents", a.apiAgents)
	mux.HandleFunc("POST /api/exec", a.apiExec)
	mux.HandleFunc("POST /api/sessions/{key}/terminal/input", a.terminalInput)
	// The terminal mirror is a raw-byte SSE stream with custom event
	// names (term/gone/ping), not one of the JSON /api/stream feeds.
	mux.HandleFunc("GET /stream/terminal/{key}", a.streamTerminal)
	mux.HandleFunc("GET /api/models", a.apiModels)
	mux.HandleFunc("GET /api/skills", a.apiSkills)
	mux.HandleFunc("GET /api/prompts", a.apiPrompts)
	mux.HandleFunc("POST /api/upload", a.uploadHandler)
	mux.HandleFunc("GET /api/push/key", a.pushKey)
	mux.HandleFunc("POST /api/push/subscribe", a.pushSubscribe)
	mux.HandleFunc("POST /api/push/unsubscribe", a.pushUnsubscribe)
	mux.HandleFunc("GET /api/quotas", a.apiQuotas)
	mux.HandleFunc("POST /api/quotas/refresh", a.apiQuotasRefresh)
	return mux
}

// sessionsWire is the JSON snapshot the React SPA boots from (rewrite-plan.md).
// Nil slices and maps marshal as null, so both fields are always non-nil.
type sessionsWire struct {
	Sessions []pi.SessionMeta `json:"sessions"`
	// Total is the matched session count after q but before limit, so a
	// windowed client knows how much lies beyond its page.
	Total  int               `json:"total"`
	Badges map[string]string `json:"badges"`
	// Driven carries the live driver state per session path: status,
	// pane title and workspace label, empty for undriven sessions.
	Driven map[string]herdr.DrivenInfo `json:"driven"`
	// Awaiting marks sessions whose agent sits in a blocking pi dialog
	// (write review, questionnaire). Keyed by session path like badges.
	Awaiting map[string]bool `json:"awaiting"`
}

// sessionUpsertCap bounds one tick's delta: more churn than this and
// the frame collapses to a full reset, keeping a pathological tick from
// dwarfing the list it patches.
const sessionUpsertCap = 64

// sessionPatch is one sessions-stream frame. The entry delta rides
// upserts (new or changed metas) and removed (client keys); a first
// frame or an over-cap tick rides the whole list in sessions. Total is
// always present: the header count lives on it. The herdr map fields
// reuse their marshaled bytes and omit when unchanged.
type sessionPatch struct {
	Sessions []pi.SessionMeta `json:"sessions,omitempty"`
	Upserts  []pi.SessionMeta `json:"upserts,omitempty"`
	Removed  []string         `json:"removed,omitempty"`
	Total    int              `json:"total"`
	Badges   json.RawMessage  `json:"badges,omitempty"`
	Driven   json.RawMessage  `json:"driven,omitempty"`
	Awaiting json.RawMessage  `json:"awaiting,omitempty"`
}

// apiSessions serves the windowed snapshot: q filters name, first_user
// and cwd (case-insensitive, the client's search rule), limit caps the
// rows after filtering, and total reports the matched count before the
// cap so the phone knows when to offer load-more.
func (a *app) apiSessions(w http.ResponseWriter, r *http.Request) {
	sessions, badges, driven, awaiting := a.snapshot(r)
	total := len(sessions)
	if needle := strings.TrimSpace(r.URL.Query().Get("q")); needle != "" {
		n := strings.ToLower(needle)
		kept := sessions[:0]
		for _, s := range sessions {
			if strings.Contains(strings.ToLower(s.Name), n) ||
				strings.Contains(strings.ToLower(s.FirstUser), n) ||
				strings.Contains(strings.ToLower(s.Cwd), n) {
				kept = append(kept, s)
			}
		}
		sessions, total = kept, len(kept)
	}
	if raw := r.URL.Query().Get("limit"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n <= 0 {
			http.Error(w, "bad limit", http.StatusBadRequest)
			return
		}
		if n < len(sessions) {
			sessions = sessions[:n]
		}
	}
	if sessions == nil {
		sessions = []pi.SessionMeta{}
	}
	if badges == nil {
		badges = map[string]string{}
	}
	if driven == nil {
		driven = map[string]herdr.DrivenInfo{}
	}
	writeJSON(w, http.StatusOK, sessionsWire{Sessions: sessions, Total: total, Badges: badges, Driven: driven, Awaiting: awaiting})
}

func (a *app) snapshot(r *http.Request) ([]pi.SessionMeta, map[string]string, map[string]herdr.DrivenInfo, map[string]bool) {
	sessions, err := a.idx.List()
	if err != nil {
		a.logger.Error("session list", "err", err)
		sessions = nil
	}
	badges, driven := a.herdr.DriveStatus(r.Context())
	return sessions, badges, driven, a.awaiting.snapshot()
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

// wakeDialog signals the stream loops that dialog state changed, so a
// pi question reaches the phone now instead of riding the next
// session-file tick or the 15s heartbeat. Nil-safe for test apps.
func (a *app) wakeDialog() {
	if a.dialogWake == nil {
		return
	}
	select {
	case a.dialogWake <- struct{}{}:
	default:
	}
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
// firehose), or on the heartbeat when nothing changes. A non-nil extra
// channel (the dialog wake) short-circuits both waits. False means the
// client left.
func (p *pacer) wait(ctx context.Context, ticks <-chan struct{}, extra <-chan struct{}) bool {
	if p.last.IsZero() {
		p.last = time.Now()
		return true
	}
	if d := streamMinInterval - time.Since(p.last); d > 0 {
		select {
		case <-ctx.Done():
			return false
		case <-extra:
			p.last = time.Now()
			return true
		case <-time.After(d):
		}
	}
	select {
	case <-ticks:
		p.last = time.Now()
		return true
	case <-extra:
		p.last = time.Now()
		return true
	case <-time.After(heartbeat):
		p.last = time.Now()
		return true
	case <-ctx.Done():
		return false
	}
}
