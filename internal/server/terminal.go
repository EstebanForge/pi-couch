package server

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"net/http"
	"strconv"
	"time"
)

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

	cols, rows := terminalDims(r)
	ch, unsub := a.observes.subscribe(target, cols, rows)
	defer unsub()
	ticker := time.NewTicker(keepaliveEvery)
	defer ticker.Stop()
	for {
		select {
		case f, ok := <-ch:
			if !ok {
				return
			}
			if f.reset {
				// This viewer stalled and was evicted: end the stream so
				// the client reconnects into a fresh full repaint.
				if !writeSSE(w, "reset", "{}") {
					return
				}
				flusher.Flush()
				return
			}
			if f.failed {
				// No herdr binary to observe with: legacy snapshot loop.
				a.pollTerminal(w, r, target)
				return
			}
			if f.closed {
				if !writeSSE(w, "gone", "{}") {
					return
				}
				flusher.Flush()
				return
			}
			frame, err := json.Marshal(map[string]string{"bytes": f.bytes})
			if err != nil || !writeSSE(w, "delta", string(frame)) {
				return
			}
		case <-ticker.C:
			if !writeSSE(w, "ping", "{}") {
				return
			}
		case <-ctx.Done():
			return
		}
	}
}

// terminalDims reads the viewer's xterm fit size so herdr frames the
// observe viewport to the same grid.
func terminalDims(r *http.Request) (int, int) {
	cols, rows := 100, 30
	if v, err := strconv.Atoi(r.URL.Query().Get("cols")); err == nil && v >= 40 && v <= 500 {
		cols = v
	}
	if v, err := strconv.Atoi(r.URL.Query().Get("rows")); err == nil && v >= 10 && v <= 200 {
		rows = v
	}
	return cols, rows
}

// pollTerminal is the pre-observe fallback: a hash-gated whole-screen
// pane.read snapshot loop, kept for boxes without the herdr CLI.
func (a *app) pollTerminal(w http.ResponseWriter, r *http.Request, target string) {
	ctx := r.Context()
	lastHash := ""
	lastSent := time.Now()
	for {
		read, err := a.herdr.PaneRead(ctx, target, "recent", 400)
		if err != nil {
			return
		}
		sum := sha256.Sum256([]byte(read.Text))
		hash := string(sum[:])
		if hash != lastHash {
			lastHash = hash
			lastSent = time.Now()
			payload, err := json.Marshal(map[string]any{"text": read.Text, "revision": read.Revision})
			if err != nil || !writeSSE(w, "term", string(payload)) {
				return
			}
		} else if time.Since(lastSent) >= keepaliveEvery {
			// A quiet pane sends nothing otherwise; the client's silence
			// watchdog would kill a healthy stream.
			lastSent = time.Now()
			if !writeSSE(w, "ping", "{}") {
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

// writeSSE emits one SSE event; false means the client is gone. Each
// frame carries a write deadline so a stalled consumer cannot pin a
// stream goroutine.
func writeSSE(w http.ResponseWriter, event, data string) bool {
	if rc := http.NewResponseController(w); rc != nil {
		_ = rc.SetWriteDeadline(time.Now().Add(sseWriteDeadline))
	}
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
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10) // keystrokes, not file uploads
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
