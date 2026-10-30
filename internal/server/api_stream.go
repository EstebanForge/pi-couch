package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"hash/fnv"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

// This file is the JSON side of the streams: the wire contract for the
// React client (rewrite-plan.md). Every stream sends reset first, so an
// EventSource reconnect resyncs without a separate snapshot fetch.
//
//	event reset  {items,status,model,thinking} | sessionPatch (entry
//	             deltas via upserts/removed/total; full sessions list
//	             on connect or over-cap ticks; maps only when changed)
//	event append {items}   transcript only, items carry seq
//	event status {status,model,thinking}       transcript only
//	event ping   keepalive

// sseWriteDeadline bounds one frame's write so a slow or dead consumer
// cannot pin a stream goroutine forever.
const sseWriteDeadline = 15 * time.Second

type transcriptReset struct {
	Items    []pi.ViewItem `json:"items"`
	Status   string        `json:"status"`
	Model    string        `json:"model"`
	Thinking string        `json:"thinking"`
	// Awaiting: dialog chrome on the pane at connect time. Omitted when
	// false.
	Awaiting bool `json:"awaiting,omitempty"`
	// Dialog is the bridge extension's open pi dialog, if any, with
	// the screen menu merged in for select kinds.
	Dialog *dialogWire `json:"dialog,omitempty"`
	// Todos is the extension todo checklist, latest state. Nil when the
	// session never ran the todo extension.
	Todos []pi.Todo `json:"todos,omitempty"`
	// Queue is pi's painted follow-up queue at connect time.
	Queue []string `json:"queue,omitempty"`
	// Memory is the agent memory server's health class, probed against
	// AGENTMEMORY_URL. Empty until the first probe lands.
	Memory string `json:"memory,omitempty"`
	// Meta names the session for the header: deep links have no list
	// snapshot to look the title up in.
	Meta *sessionHeader `json:"meta,omitempty"`
}

type sessionHeader struct {
	Name       string  `json:"name"`
	FirstUser  string  `json:"first_user"`
	Cwd        string  `json:"cwd"`
	CtxTokens  int64   `json:"ctx_tokens,omitempty"`
	TotalCost  float64 `json:"total_cost,omitempty"`
	LastUserAt int64   `json:"last_user_at,omitempty"`
}

func headerOf(m pi.SessionMeta) *sessionHeader {
	return &sessionHeader{Name: m.Name, FirstUser: m.FirstUser, Cwd: m.Cwd, CtxTokens: m.CtxTokens, TotalCost: m.TotalCost, LastUserAt: m.LastUserAt}
}

type transcriptAppend struct {
	Items []pi.ViewItem `json:"items"`
}

type transcriptStatus struct {
	Status   string `json:"status"`
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
	// CtxTokens rides the newest turn's usage so the composer can
	// render a live context-window fill.
	CtxTokens int64 `json:"ctx_tokens,omitempty"`
	// Awaiting: the agent sits in a blocking pi dialog while still
	// reporting working. Omitted when false.
	Awaiting bool `json:"awaiting,omitempty"`
	// Dialog carries the open pi dialog (bridge identity, screen parse
	// attached). Never omitempty: an absent key would read as
	// keep-on-the-client, and only an explicit null may clear a card
	// once pi's end event lands.
	Dialog *dialogWire `json:"dialog"`
	// Todos re-sent whenever the checklist changes. Never omitempty:
	// a checklist clear must reach the client as an empty array, not
	// as an absent key that reads as keep-what-you-have.
	Todos []pi.Todo `json:"todos"`
	// Queue is pi's follow-up queue. Never omitempty, same rule as
	// Todos: a drained queue must arrive as an empty array.
	Queue []string `json:"queue"`
	// Memory is the agent memory server's health class; rides frames
	// whenever it changes.
	Memory string `json:"memory,omitempty"`
}

// dialogWire is the open pi dialog as the phone sees it: the bridge
// entry's identity, plus the option rows parsed off the pane when the
// dialog is a select. The cursor rides along so the card can highlight
// where pi's selection sits.
type dialogOptionWire struct {
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
}

type dialogWire struct {
	ID      string             `json:"id,omitempty"`
	Kind    string             `json:"kind"`
	Title   string             `json:"title"`
	Body    string             `json:"body,omitempty"`
	Options []dialogOptionWire `json:"options,omitempty"`
	Cursor  int                `json:"cursor"`
	// Scrolled marks an editor whose buffer outgrew pi's viewport: the
	// body is the painted tail only, so the card must not prefill from
	// it - the hidden head would be silently lost on a rewrite.
	Scrolled bool `json:"scrolled,omitempty"`
}

// dialogWireFor composes the bridge entry with the pane parse. The
// parser reads every pi hint family, so the card gets its question,
// body and rows whatever kind the bridge reported (the rpiv
// questionnaire reports "custom" with no title; only the pane knows
// what it asks). The model picker never parses - answering it would
// save a default.
func (a *app) dialogWireFor(path string, d BridgeDialog, ok bool) *dialogWire {
	if !ok {
		a.forgetDialogParse(path)
		return nil
	}
	w := &dialogWire{ID: d.ID, Kind: d.Kind, Title: d.Title}
	if p := a.awaiting.parsed(path); p != nil {
		if w.Title == "" {
			w.Title = p.Title
		}
		w.Body = p.Body
		w.Cursor = p.Cursor
		w.Scrolled = p.Scrolled
		if p.Scrolled && p.Kind == "editor" {
			// The pane only paints a window of the buffer, but the tool
			// call behind the dialog carries the whole draft - prefill
			// from it; walking the pane's cursor risks pi's history.
			if m, ok := a.idx.ByPath(path); ok {
				if draft := a.idx.ToolCallDraft(m.Key); draft != "" {
					w.Body = draft
					w.Scrolled = false
				} else {
					// No draft in reach: show the painted tail without
					// pi's divider and scroll chrome around it.
					w.Body = stripEditorChrome(w.Body)
				}
			}
		}
		for _, o := range p.Options {
			w.Options = append(w.Options, dialogOptionWire{Label: o.Label, Description: o.Description})
		}
		if w.Kind == "editor" && w.Title == "" {
			w.Title = "Commit message"
		}
		a.rememberDialogParse(path, w)
		return w
	}
	// A single tick's pane read can flake (mid-repaint, a wrapping
	// rule); dropping the body then closes the phone card mid-answer,
	// so the last good composition for THIS dialog id is served
	// until the parse recovers.
	if l := a.latchedDialogParse(path); l != nil && l.ID == d.ID {
		return l
	}
	if w.Kind == "editor" && w.Title == "" {
		w.Title = "Commit message"
	}
	return w
}

func (a *app) rememberDialogParse(path string, w *dialogWire) {
	if a.dialogLatch == nil {
		return
	}
	a.dialogMu.Lock()
	a.dialogLatch[path] = w
	a.dialogMu.Unlock()
}

func (a *app) latchedDialogParse(path string) *dialogWire {
	a.dialogMu.Lock()
	defer a.dialogMu.Unlock()
	return a.dialogLatch[path]
}

func (a *app) forgetDialogParse(path string) {
	if a.dialogLatch == nil {
		return
	}
	a.dialogMu.Lock()
	delete(a.dialogLatch, path)
	a.dialogMu.Unlock()
}

// dialogEqual compares wire dialogs so the status event fires when the
// body, options or cursor move, not only when the bridge entry
// changes.
func dialogEqual(a, b *dialogWire) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.ID != b.ID || a.Kind != b.Kind || a.Title != b.Title ||
		a.Body != b.Body || a.Cursor != b.Cursor || a.Scrolled != b.Scrolled || len(a.Options) != len(b.Options) {
		return false
	}
	for i := range a.Options {
		if a.Options[i] != b.Options[i] {
			return false
		}
	}
	return true
}

// startSSE writes the event-stream headers and flushes them. The caller
// owns the loop from there.
func startSSE(w http.ResponseWriter) bool {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	f, ok := w.(http.Flusher)
	if !ok {
		return false
	}
	f.Flush()
	return true
}

// writeEventJSON emits one named frame carrying a JSON payload, over the
// shared raw writer so the write deadline applies here too.
func writeEventJSON(w http.ResponseWriter, event string, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if !writeSSE(w, event, string(b)) {
		return errClientGone
	}
	return nil
}

// apiStreamTranscript is the JSON twin of streamTranscript: reset, then
// appends for growth, status on driver/model/thinking change, ping when
// idle. The fingerprint logic is shared with the templ stream.
func (a *app) apiStreamTranscript(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	limit := defaultTail
	if n, err := parseLimit(r); err == nil {
		limit = n
	}
	if !startSSE(w) {
		return
	}
	ticks, unsub := a.subscribeTicks()
	defer unsub()
	wake, unsubWake := a.subscribeWake()
	defer unsubWake()
	var pace pacer
	send := func(event string, v any) bool { return writeEventJSON(w, event, v) == nil }

	prev := []uint32{}
	prevFirstSeq := 0
	status, model, thinking := "", "", ""
	var lastAwaiting bool
	var lastTodos []pi.Todo
	var lastQueue []string
	var lastCtx int64
	var lastDialog *dialogWire
	var lastSend time.Time
	placeholder := false
	for {
		meta, ok := a.idx.Meta(key)
		if !ok {
			// pi reports a session's path at agent start, before the first
			// write lands the jsonl. Hold the window open with one empty
			// reset instead of returning, so a fresh agent's transcript is
			// never a dead window; a bogus key just idles on heartbeats.
			if !placeholder {
				if !send("reset", transcriptReset{Items: []pi.ViewItem{}}) {
					return
				}
				placeholder = true
			}
			if !pace.wait(r.Context(), ticks, wake) {
				return
			}
			continue
		}
		_, items, err := a.idx.Transcript(key, limit)
		if err != nil {
			return
		}
		cur := itemFps(items)
		mem := a.memory.class(r.Context())
		badges := a.statusFor().Badges(r.Context())
		st := badges[meta.Path]
		awaiting := a.awaiting.isAwaiting(meta.Path)
		queue := a.awaiting.queued(meta.Path)
		dialog, hasDialog := a.dialogFor(meta.Path, st)
		dw := a.dialogWireFor(meta.Path, dialog, hasDialog)
		forceReset := placeholder
		placeholder = false
		sent := false
		if delta, ok := chatAppend(prev, items, cur, prevFirstSeq); ok && !forceReset {
			if len(delta) > 0 {
				if !send("append", transcriptAppend{Items: delta}) {
					return
				}
				prev = cur
				prevFirstSeq = items[0].Seq
				sent = true
			}
		} else {
			if !send("reset", transcriptReset{Items: items, Status: st, Model: meta.Model, Thinking: meta.Thinking, Awaiting: awaiting, Dialog: dw, Todos: meta.Todos, Queue: queue, Memory: mem, Meta: headerOf(meta)}) {
				return
			}
			prev = cur
			prevFirstSeq = 0
			if len(items) > 0 {
				prevFirstSeq = items[0].Seq
			}
			status, model, thinking = st, meta.Model, meta.Thinking
			lastQueue = queue
			sent = true
		}
		if st != status || meta.Model != model || meta.Thinking != thinking || awaiting != lastAwaiting || !slices.Equal(meta.Todos, lastTodos) || meta.CtxTokens != lastCtx || !dialogEqual(dw, lastDialog) || !slices.Equal(queue, lastQueue) {
			status, model, thinking, lastAwaiting, lastTodos, lastCtx = st, meta.Model, meta.Thinking, awaiting, meta.Todos, meta.CtxTokens
			lastDialog = dw
			lastQueue = queue
			if !send("status", transcriptStatus{Status: st, Model: meta.Model, Thinking: meta.Thinking, CtxTokens: meta.CtxTokens, Awaiting: awaiting, Todos: meta.Todos, Queue: queue, Dialog: dw, Memory: mem}) {
				return
			}
			sent = true
		}
		if !sent && !lastSend.IsZero() && time.Since(lastSend) >= keepaliveEvery {
			if !send("ping", nil) {
				return
			}
			sent = true
		}
		if sent {
			lastSend = time.Now()
		}
		if !pace.wait(r.Context(), ticks, wake) {
			return
		}
	}
}

// apiStreamSessions is the JSON twin of streamSessions. Frames carry
// entry deltas: upserts for new or changed metas, removed for gone
// ones, total always, and each herdr map only when its bytes changed.
// A first frame or a tick touching more than sessionUpsertCap entries
// collapses to a full reset. The REST snapshot stays whole.
func (a *app) apiStreamSessions(w http.ResponseWriter, r *http.Request) {
	if !startSSE(w) {
		return
	}
	ticks, unsub := a.subscribeTicks()
	defer unsub()
	wake, unsubWake := a.subscribeWake()
	defer unsubWake()
	var pace pacer
	type entryHash struct {
		hash uint64
		key  string
	}
	var lastEntries map[string]entryHash
	var lastTotal = -1
	var lastBadges, lastDriven, lastAwaiting []byte
	field := func(cur []byte, last *[]byte) json.RawMessage {
		if *last != nil && string(*last) == string(cur) {
			return nil
		}
		*last = cur
		return json.RawMessage(cur)
	}
	for {
		sessions, badges, driven, awaiting := a.snapshot(r)
		if sessions == nil {
			sessions = []pi.SessionMeta{}
		}
		if badges == nil {
			badges = map[string]string{}
		}
		if driven == nil {
			driven = map[string]herdr.DrivenInfo{}
		}
		if awaiting == nil {
			awaiting = map[string]bool{}
		}
		entries := make(map[string]entryHash, len(sessions))
		var upserts []pi.SessionMeta
		for _, m := range sessions {
			b, err := json.Marshal(m)
			if err != nil {
				return
			}
			h := fnv.New64a()
			h.Write(b)
			entries[m.Path] = entryHash{h.Sum64(), m.Key}
			if prev, ok := lastEntries[m.Path]; !ok || prev.hash != h.Sum64() {
				upserts = append(upserts, m)
			}
		}
		var removed []string
		for path, e := range lastEntries {
			if _, ok := entries[path]; !ok {
				removed = append(removed, e.key)
			}
		}
		reset := lastEntries == nil || len(upserts) > sessionUpsertCap
		lastEntries = entries
		// A herdr-only change (dialog opens, driver flips blocked) touches
		// no session file, so the map fields must open the gate themselves
		// or the phone sits on stale status until some unrelated write.
		emit := false
		patch := sessionPatch{Total: len(sessions)}
		if lastTotal != len(sessions) {
			emit = true
		}
		lastTotal = len(sessions)
		if reset {
			patch.Sessions = sessions
			// A reset re-baselines the client, so the map gates must
			// ride too or a stale client would keep pre-reset maps.
			lastBadges, lastDriven, lastAwaiting = nil, nil, nil
			emit = true
		} else {
			if len(upserts) > 0 {
				patch.Upserts = upserts
				emit = true
			}
			if len(removed) > 0 {
				patch.Removed = removed
				emit = true
			}
		}
		bb, _ := json.Marshal(badges)
		db, _ := json.Marshal(driven)
		ab, _ := json.Marshal(awaiting)
		if patch.Badges = field(bb, &lastBadges); patch.Badges != nil {
			emit = true
		}
		if patch.Driven = field(db, &lastDriven); patch.Driven != nil {
			emit = true
		}
		if patch.Awaiting = field(ab, &lastAwaiting); patch.Awaiting != nil {
			emit = true
		}
		if emit {
			if writeEventJSON(w, "reset", patch) != nil {
				return
			}
		}
		if !pace.wait(r.Context(), ticks, wake) {
			return
		}
	}
}

func parseLimit(r *http.Request) (int, error) {
	n, err := strconv.Atoi(r.URL.Query().Get("n"))
	if err != nil || n <= 0 || n > 1000 {
		return 0, fmt.Errorf("bad n")
	}
	return n, nil
}

// --- JSON steering twins ---

var (
	errStopFail   = errors.New("stop failed")
	errCloseFail  = errors.New("close failed")
	errEmptyText  = errors.New("empty text")
	errPromptFail = errors.New("prompt failed")
)

// stopAgent sends ctrl+c to the herdr agent driving the session. Shared
// by the templ form handler and the JSON endpoint.
func (a *app) stopAgent(key string) error {
	meta, ok := a.idx.Meta(key)
	if !ok {
		return errNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, err := a.statusFor().TargetForPath(ctx, meta.Path)
	if err != nil {
		return herdr.ErrNotDriven
	}
	if err := a.herdr.SendKeys(ctx, target, []string{"ctrl+c"}); err != nil {
		a.logger.Error("stop", "err", err)
		return errStopFail
	}
	return nil
}

// closeWorkspace closes the herdr workspace driving the session. The
// jsonl stays as a read-only archive.
func (a *app) closeWorkspace(key string) error {
	meta, ok := a.idx.Meta(key)
	if !ok {
		return errNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	paneID, err := a.statusFor().TargetForPath(ctx, meta.Path)
	if err != nil {
		return herdr.ErrNotDriven
	}
	// Graceful pane-level close: ctrl+d exits pi, and once the shell
	// is back a second ctrl+d ends it — the pane dies with its shell,
	// so sibling agents in the same workspace survive (workspace.close
	// would tear them all down).
	if err := a.herdr.PaneSendInput(ctx, paneID, "", []string{"ctrl+d"}); err != nil {
		return errCloseFail
	}
	select {
	case <-time.After(time.Second):
	case <-ctx.Done():
		return ctx.Err()
	}
	if err := a.herdr.PaneSendInput(ctx, paneID, "", []string{"ctrl+d"}); err != nil {
		return errCloseFail
	}
	// Best effort: remove the pane if herdr kept it after the shell died.
	_ = a.herdr.PaneClose(ctx, paneID)
	return nil
}

var errNotFound = errors.New("session not found")

var errClientGone = errors.New("client gone")

func (a *app) apiStop(w http.ResponseWriter, r *http.Request) {
	if !a.requireHerdr(w) {
		return
	}
	a.writeOutcome(w, a.stopAgent(r.PathValue("key")))
}

// apiPrompt is the JSON twin of the templ steerPrompt: fetch cannot
// read the 303 redirects, so the outcome is in-band.
func (a *app) apiPrompt(w http.ResponseWriter, r *http.Request) {
	if !a.requireHerdr(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var in struct {
		Text     string `json:"text"`
		WhenBusy string `json:"whenBusy"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "badrequest"})
		return
	}
	// whenBusy carries the submitter's disposition. herdr exposes one
	// verb, so follow-up is the only placement that exists; refusing
	// the rest keeps the wire honest instead of guessing pi's queue
	// mode (ADR-0001).
	switch in.WhenBusy {
	case "", "followUp":
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "badrequest"})
		return
	}
	err := a.promptAgent(r.PathValue("key"), in.Text)
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case errors.Is(err, errEmptyText):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "empty"})
	case errors.Is(err, errNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
	case errors.Is(err, herdr.ErrNotDriven):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "notdriven"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed"})
	}
}

func (a *app) apiClose(w http.ResponseWriter, r *http.Request) {
	if !a.requireHerdr(w) {
		return
	}
	a.writeOutcome(w, a.closeWorkspace(r.PathValue("key")))
}

func (a *app) writeOutcome(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case errors.Is(err, errNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
	case errors.Is(err, herdr.ErrNotDriven):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "notdriven"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed"})
	}
}
