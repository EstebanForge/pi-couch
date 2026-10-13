package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/store"
)

const maxBatchComments = 50

// sendAllInput is the island's Send All payload.
type sendAllInput struct {
	Comments []struct {
		File string `json:"file"`
		Line int    `json:"line"`
		Text string `json:"text"`
	} `json:"comments"`
}

// sendAll formats the tray into one review prompt, submits it through
// herdr prompt, and only then records it as sent. A failed or undriven
// send persists nothing, so the device tray stays the source of truth.
func (a *app) apiSendAll(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	meta, ok := a.idx.Meta(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	var in sendAllInput
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&in); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if len(in.Comments) == 0 || len(in.Comments) > maxBatchComments {
		http.Error(w, "empty or oversized batch", http.StatusBadRequest)
		return
	}
	var b strings.Builder
	b.WriteString("Review comments on changes:\n")
	for _, c := range in.Comments {
		body := strings.TrimSpace(c.Text)
		file := strings.TrimSpace(c.File)
		if body == "" || file == "" {
			http.Error(w, "comment missing file or text", http.StatusBadRequest)
			return
		}
		fmt.Fprintf(&b, "- %s:%d: %s\n", file, c.Line, body)
	}
	b.WriteString("Please address these items.")

	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		if errors.Is(err, herdr.ErrNotDriven) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "notdriven"})
		} else {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "target"})
		}
		return
	}
	if err := a.herdr.Prompt(ctx, target, b.String()); err != nil {
		slog.Error("send comments prompt", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "prompt"})
		return
	}
	sent := make([]store.Comment, len(in.Comments))
	for i, c := range in.Comments {
		sent[i] = store.Comment{File: strings.TrimSpace(c.File), Line: c.Line, Body: strings.TrimSpace(c.Text)}
	}
	if err := a.st.Append(meta.Key, sent); err != nil {
		// The agent got the prompt; record the failure but report success
		// so the tray clears and the user does not double-send.
		slog.Error("persist sent comments", "err", err)
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
