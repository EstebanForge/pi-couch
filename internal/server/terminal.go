package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/EstebanForge/piremote/internal/web/pages"
)

func (a *app) terminal(w http.ResponseWriter, r *http.Request) {
	meta, ok := a.idx.Meta(r.PathValue("key"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	badges := a.herdr.Badges(r.Context())
	serveTempl(w, r, pages.Terminal(meta, badges[meta.Path]))
}

// streamTerminal polls pane.read for the session's pane and emits the
// visible screen when the revision moves. herdr 0.9.3 offers no output
// push (subscribe sockets ack only; events.wait is agent-status only).
func (a *app) streamTerminal(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	meta, ok := a.idx.Meta(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	flusher := http.NewResponseController(w)
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")

	ctx := r.Context()
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		writeSSE(w, "gone", "{}")
		flusher.Flush()
		return
	}

	lastRev := int64(-1)
	for {
		read, err := a.herdr.PaneRead(ctx, target, "recent", 400)
		if err != nil {
			return
		}
		if read.Revision != lastRev {
			lastRev = read.Revision
			payload, err := json.Marshal(map[string]any{"text": read.Text, "revision": read.Revision})
			if err != nil || !writeSSE(w, "term", string(payload)) {
				return
			}
		}
		select {
		case <-time.After(terminalPoll):
		case <-ctx.Done():
			return
		}
	}
}

// writeSSE emits one SSE event; false means the client is gone.
func writeSSE(w http.ResponseWriter, event, data string) bool {
	_, err := w.Write([]byte("event: " + event + "\ndata: " + data + "\n\n"))
	if err != nil {
		return false
	}
	http.NewResponseController(w).Flush()
	return true
}

// terminalInput forwards wheel input: text runs, or named keys.
func (a *app) terminalInput(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	meta, ok := a.idx.Meta(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var in struct {
		Text string   `json:"text"`
		Keys []string `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil || (in.Text == "" && len(in.Keys) == 0) {
		http.Error(w, "empty input", http.StatusBadRequest)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		http.Error(w, "no driver", http.StatusConflict)
		return
	}
	if err := a.herdr.PaneSendInput(ctx, target, in.Text, in.Keys); err != nil {
		a.logger.Error("terminal input", "err", err)
		http.Error(w, "input failed", http.StatusBadGateway)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
