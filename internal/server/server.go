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
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EstebanForge/couch-pi/internal/config"
	"github.com/EstebanForge/couch-pi/internal/hc"
	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
	"github.com/EstebanForge/couch-pi/internal/push"
	"github.com/EstebanForge/couch-pi/internal/quotas"
	"github.com/EstebanForge/couch-pi/internal/store"
	"github.com/EstebanForge/couch-pi/internal/update"
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
	status        StatusReader // the active status surface; nil falls back to herdr
	hcDrv         *hc.Driver   // hc dialog primitives; nil under herdr
	watcher       *pi.Watcher  // nil: heartbeat-only updates
	st            *store.Store
	pusher        *push.Sender
	vapidPublic   string
	allowedOrigin string // config origin; the public https origin served through tailscale
	accessToken   string // shared secret gating /api and /stream; empty means open
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
	wakeMu        sync.Mutex
	wakeSubs      map[chan struct{}]struct{} // per-stream wake channels; a dialog changed
	observes      *observeHub                // per-pane `herdr observe` subprocesses fanned to terminal viewers
	quotas        *quotas.Service            // provider quotas & limits tracker
}

var _ StatusReader = (*hc.Driver)(nil)

// StatusReader is the per-tick status surface both drivers serve
// (ADR-0008): herdr.Client satisfies it with socket dials, hc.Driver
// from its label map without dials.
type StatusReader interface {
	Badges(ctx context.Context) map[string]string
	Driven(ctx context.Context) map[string]herdr.DrivenInfo
	DriveStatus(ctx context.Context) (map[string]string, map[string]herdr.DrivenInfo)
	TargetForPath(ctx context.Context, path string) (string, error)
}

func (a *app) statusFor() StatusReader {
	if a.status != nil {
		return a.status
	}
	return a.herdr
}

// CommandDriver is the in-session command surface both drivers serve
// (ADR-0008 wave 4): prompt, abort, close, addressed by the driver
// target TargetForPath hands out.
type CommandDriver interface {
	Prompt(ctx context.Context, target, text string) error
	Stop(ctx context.Context, target string) error
	Close(ctx context.Context, target string) error
}

var _ CommandDriver = (*herdr.Client)(nil)
var _ CommandDriver = (*hc.Driver)(nil)

// cmdFor returns the command surface: herdr when wired, else the hc
// driver. Nil means no driver can take commands.
// cmdBudget keeps hc's raised handler deadlines off the herdr path.
func (a *app) cmdBudget(hcBudget, herdrBudget time.Duration) time.Duration {
	if a.hcDrv != nil {
		return hcBudget
	}
	return herdrBudget
}

func (a *app) cmdFor() CommandDriver {
	// hc wins even when a herdr client is also non-nil: an hc address
	// must never reach the herdr socket.
	if a.hcDrv != nil {
		return a.hcDrv
	}
	if a.herdr != nil {
		return a.herdr
	}
	if d, ok := a.status.(CommandDriver); ok {
		return d
	}
	return nil
}

// requireHerdr gates the herdr-only surfaces while the hc waves land:
// the hc state is herdr==nil with an explicit status driver wired.
// Literals with neither (tests) keep exercising the herdr paths.
func (a *app) requireHerdr(w http.ResponseWriter) bool {
	if a.herdr != nil || a.status == nil {
		return true
	}
	writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "unsupported-driver"})
	return false
}

func Run(ctx context.Context, logger *slog.Logger, cfg *config.Config, idx *pi.Index, hr *herdr.Client, status StatusReader, w *pi.Watcher, st *store.Store, pusher *push.Sender) error {
	home, _ := os.UserHomeDir()
	if status == nil {
		status = hr
	}
	a := &app{logger: logger, idx: idx, herdr: hr, status: status, watcher: w, st: st, pusher: pusher, allowedOrigin: cfg.Origin, accessToken: cfg.AccessToken, uploadDir: cfg.UploadsDir, home: home, answered: newDialogMemos(), models: newModelsCache(filepath.Join(st.Dir(), "models.json")), memory: newMemoryProbe(cfg.MemoryURL, cfg.MemorySecret), dialogLatch: map[string]*dialogWire{}, quotas: quotas.NewService()}
	if hr != nil {
		// Terminal mirroring stays herdr-only until its attach wave.
		a.observes = newObserveHub(hr.SocketPath())
	}
	if drv, ok := status.(*hc.Driver); ok {
		// hc keeps the pane watching too: the driver renders the labeled
		// terminals as the detector's agent source (ADR-0008 wave 5).
		// Only the observe hub waits for the attach wave.
		a.hcDrv = drv
		a.awaiting = newAwaitingDetector(drv, time.Second)
		a.awaiting.onChange = a.wakeDialog
		go a.awaiting.run(ctx)
	}
	if hr != nil && a.awaiting == nil {
		a.awaiting = newAwaitingDetector(hr, time.Second)
		a.awaiting.onChange = a.wakeDialog
		go a.awaiting.run(ctx)
	}
	if pusher != nil {
		a.vapidPublic = pusher.Public
	}
	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           a.guard(a.auth(a.routes())),
		ReadHeaderTimeout: 5 * time.Second,
	}
	logger.Info("couch-pi listening", "addr", cfg.Addr)
	ln, err := net.Listen("tcp", cfg.Addr)
	if err != nil {
		return err
	}
	// A bound listener is what proves an updated binary healthy: the
	// rollback marker clears here, not on a timer.
	if dir, derr := store.Dir(); derr == nil {
		update.ProveBoot(dir)
	}
	srvErr := make(chan error, 1)
	go func() {
		err := srv.Serve(ln)
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
	mux.HandleFunc("GET /api/sessions/{key}/preview", a.apiSessionPreview)
	mux.HandleFunc("GET /api/sessions/{key}/files", a.apiFiles)
	mux.HandleFunc("GET /api/sessions/{key}/files/view", a.apiFilesView)
	mux.HandleFunc("POST /api/sessions/{key}/comments", a.apiSendAll)
	mux.HandleFunc("POST /api/sessions/{key}/stop", a.apiStop)
	mux.HandleFunc("POST /api/sessions/{key}/close", a.apiClose)
	mux.HandleFunc("POST /api/sessions/{key}/prompt", a.apiPrompt)
	mux.HandleFunc("POST /api/sessions/{key}/resume", a.apiSessionResume)
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
	mux.HandleFunc("POST /api/models/refresh", a.apiModelsRefresh)
	mux.HandleFunc("GET /api/skills", a.apiSkills)
	mux.HandleFunc("GET /api/prompts", a.apiPrompts)
	mux.HandleFunc("POST /api/upload", a.uploadHandler)
	mux.HandleFunc("GET /api/push/key", a.pushKey)
	mux.HandleFunc("POST /api/push/subscribe", a.pushSubscribe)
	mux.HandleFunc("POST /api/push/unsubscribe", a.pushUnsubscribe)
	mux.HandleFunc("GET /api/quotas", a.apiQuotas)
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
	badges, driven := a.statusFor().DriveStatus(r.Context())
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
	// hc's send pipeline paces itself (quiet, settle, first-paint
	// retry), so the ceiling is generous; herdr answers in milliseconds.
	ctx, cancel := context.WithTimeout(context.Background(), a.cmdBudget(25*time.Second, 10*time.Second))
	defer cancel()
	target, err := a.statusFor().TargetForPath(ctx, meta.Path)
	if err != nil {
		return herdr.ErrNotDriven
	}
	cmd := a.cmdFor()
	if cmd == nil {
		return herdr.ErrNotDriven
	}
	if err := cmd.Prompt(ctx, target, text); err != nil {
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

// chatAppend decides whether the freshly read window grew out of the
// previous one. The window is a fixed-size tail, so growth also drops
// the oldest items; Seq gives the exact slide, and the fingerprint
// compare over the overlap catches in-place edits that a pure seq
// count would bless. Content-only alignment is not enough: repetitive
// windows (polling agents repeating one tool pair) alias at the wrong
// drop and silently lose items as an empty delta.
func chatAppend(prev []uint32, items []pi.ViewItem, cur []uint32, prevFirstSeq int) (delta []pi.ViewItem, ok bool) {
	if len(prev) == 0 || len(items) < len(prev) || len(cur) != len(items) || len(items) == 0 {
		return nil, false
	}
	drop := items[0].Seq - prevFirstSeq
	if drop < 0 || drop >= len(prev) {
		return nil, false
	}
	if !slices.Equal(prev[drop:], cur[:len(prev)-drop]) {
		return nil, false
	}
	return items[len(prev)-drop:], true
}

// subscribeTicks registers one fan-out slot; nil watcher degrades to
// heartbeat-only updates.
func (a *app) subscribeTicks() (<-chan struct{}, func()) {
	if a.watcher == nil {
		return nil, func() {}
	}
	return a.watcher.Subscribe()
}

// subscribeWake hands each stream loop its own capacity-1 channel: a
// single shared channel delivered a wake to one ready subscriber, so a
// second open stream waited out the whole tick after a dialog opened.
func (a *app) subscribeWake() (<-chan struct{}, func()) {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	if a.wakeSubs == nil {
		a.wakeSubs = make(map[chan struct{}]struct{})
	}
	ch := make(chan struct{}, 1)
	a.wakeSubs[ch] = struct{}{}
	return ch, func() {
		a.wakeMu.Lock()
		delete(a.wakeSubs, ch)
		a.wakeMu.Unlock()
	}
}

// wakeDialog signals every stream loop that dialog state changed, so a
// pi question reaches the phone now instead of riding the next
// session-file tick or the 15s heartbeat.
func (a *app) wakeDialog() {
	a.wakeMu.Lock()
	defer a.wakeMu.Unlock()
	for ch := range a.wakeSubs {
		select {
		case ch <- struct{}{}:
		default:
		}
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
