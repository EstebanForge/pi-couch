package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
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
//	event reset  {items,status,model,thinking} | sessionsWire
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
	// Meta names the session for the header: deep links have no list
	// snapshot to look the title up in.
	Meta *sessionHeader `json:"meta,omitempty"`
}

type sessionHeader struct {
	Name      string `json:"name"`
	FirstUser string `json:"first_user"`
	Cwd       string `json:"cwd"`
}

func headerOf(m pi.SessionMeta) *sessionHeader {
	return &sessionHeader{Name: m.Name, FirstUser: m.FirstUser, Cwd: m.Cwd}
}

type transcriptAppend struct {
	Items []pi.ViewItem `json:"items"`
}

type transcriptStatus struct {
	Status   string `json:"status"`
	Model    string `json:"model"`
	Thinking string `json:"thinking"`
	// Awaiting: the agent sits in a blocking pi dialog while still
	// reporting working. Omitted when false.
	Awaiting bool `json:"awaiting,omitempty"`
	// Dialog carries the bridge-reported open dialog, with the screen
	// menu merged in for select kinds.
	Dialog *dialogWire `json:"dialog,omitempty"`
	// Todos re-sent whenever the checklist changes.
	Todos []pi.Todo `json:"todos,omitempty"`
}

// dialogWire is the open pi dialog as the phone sees it: the bridge
// entry's identity, plus the option rows parsed off the pane when the
// dialog is a select. The cursor rides along so the card can highlight
// where pi's selection sits.
type dialogWire struct {
	ID      string   `json:"id,omitempty"`
	Kind    string   `json:"kind"`
	Title   string   `json:"title"`
	Options []string `json:"options,omitempty"`
	Cursor  int      `json:"cursor"`
}

// dialogWireFor composes the bridge entry with the detector's screen
// menu. Options attach to select kinds only: a confirm card needs no
// rows, and the model picker's TUI-drawn rows must never become answer
// buttons (answering there saves a default).
func (a *app) dialogWireFor(path string, d BridgeDialog, ok bool) *dialogWire {
	if !ok {
		return nil
	}
	w := &dialogWire{ID: d.ID, Kind: d.Kind, Title: d.Title}
	if d.Kind == "select" {
		if m := a.awaiting.menu(path); m != nil {
			w.Options = m.Options
			w.Cursor = m.Cursor
		}
	}
	return w
}

// dialogEqual compares wire dialogs so the status event fires when the
// options or cursor move, not only when the bridge entry changes.
func dialogEqual(a, b *dialogWire) bool {
	if a == nil || b == nil {
		return a == b
	}
	return a.ID == b.ID && a.Kind == b.Kind && a.Title == b.Title &&
		a.Cursor == b.Cursor && slices.Equal(a.Options, b.Options)
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
	var pace pacer
	send := func(event string, v any) bool { return writeEventJSON(w, event, v) == nil }

	prev := []uint32{}
	status, model, thinking := "", "", ""
	var lastAwaiting bool
	var lastTodos []pi.Todo
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
			if !pace.wait(r.Context(), ticks) {
				return
			}
			continue
		}
		_, items, err := a.idx.Transcript(key, limit)
		if err != nil {
			return
		}
		cur := itemFps(items)
		badges := a.herdr.Badges(r.Context())
		st := badges[meta.Path]
		awaiting := a.awaiting.isAwaiting(meta.Path)
		dialog, hasDialog := a.dialogFor(meta.Path, st)
		dw := a.dialogWireFor(meta.Path, dialog, hasDialog)
		forceReset := placeholder
		placeholder = false
		sent := false
		if delta, ok := chatAppend(prev, items, cur); ok && !forceReset {
			if len(delta) > 0 {
				if !send("append", transcriptAppend{Items: delta}) {
					return
				}
				prev = cur
				sent = true
			}
		} else {
			if !send("reset", transcriptReset{Items: items, Status: st, Model: meta.Model, Thinking: meta.Thinking, Awaiting: awaiting, Dialog: dw, Todos: meta.Todos, Meta: headerOf(meta)}) {
				return
			}
			prev = cur
			status, model, thinking = st, meta.Model, meta.Thinking
			sent = true
		}
		if st != status || meta.Model != model || meta.Thinking != thinking || awaiting != lastAwaiting || !slices.Equal(meta.Todos, lastTodos) || !dialogEqual(dw, lastDialog) {
			status, model, thinking, lastAwaiting, lastTodos = st, meta.Model, meta.Thinking, awaiting, meta.Todos
			lastDialog = dw
			if !send("status", transcriptStatus{Status: st, Model: meta.Model, Thinking: meta.Thinking, Awaiting: awaiting, Todos: meta.Todos, Dialog: dw}) {
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
		if !pace.wait(r.Context(), ticks) {
			return
		}
	}
}

// apiStreamSessions is the JSON twin of streamSessions. It always sends
// the full snapshot; a hash gate skips frames whose payload did not
// change, so the firehose of ticks costs one marshal per second at most.
func (a *app) apiStreamSessions(w http.ResponseWriter, r *http.Request) {
	if !startSSE(w) {
		return
	}
	ticks, unsub := a.subscribeTicks()
	defer unsub()
	var pace pacer
	var last []byte
	for {
		sessions, badges, awaiting := a.snapshot(r)
		if sessions == nil {
			sessions = []pi.SessionMeta{}
		}
		if badges == nil {
			badges = map[string]string{}
		}
		if awaiting == nil {
			awaiting = map[string]bool{}
		}
		b, err := json.Marshal(sessionsWire{Sessions: sessions, Badges: badges, Awaiting: awaiting})
		if err != nil {
			return
		}
		if last == nil || string(b) != string(last) {
			if writeEventJSON(w, "reset", json.RawMessage(b)) != nil {
				return
			}
			last = b
		}
		if !pace.wait(r.Context(), ticks) {
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
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
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
	ws, err := a.herdr.WorkspaceForPath(ctx, meta.Path)
	if err != nil {
		return herdr.ErrNotDriven
	}
	if err := a.herdr.WorkspaceClose(ctx, ws); err != nil {
		a.logger.Error("close", "err", err)
		return errCloseFail
	}
	return nil
}

var errNotFound = errors.New("session not found")

var errClientGone = errors.New("client gone")

func (a *app) apiStop(w http.ResponseWriter, r *http.Request) {
	a.writeOutcome(w, a.stopAgent(r.PathValue("key")))
}

// apiPrompt is the JSON twin of the templ steerPrompt: fetch cannot
// read the 303 redirects, so the outcome is in-band.
func (a *app) apiPrompt(w http.ResponseWriter, r *http.Request) {
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
