package server

import (
	"encoding/json"
	"net/http"
	"time"
)

// The couch-pi bridge extension (extensions/couch-pi-bridge.ts) posts
// pi's ui_prompt_start / ui_prompt_end events here so the phone can
// show which dialog pi is waiting on - herdr carries the blocked
// status but drops the label. Fire-and-forget on the extension side:
// dialogs expire by TTL when an end event is lost.

const bridgeTTL = 10 * time.Minute

// BridgeDialog is one open blocking pi dialog, by session path.
type BridgeDialog struct {
	// ID is the per-dialog id the extension generates at start; the
	// answer endpoint refuses keys unless it still matches.
	ID    string `json:"id,omitempty"`
	Kind  string `json:"kind"`  // select, confirm, input, editor, custom
	Title string `json:"title"` // dialog title, or the kind when untitled
}

// dialogFor gates the bridge dialog on herdr's blocked status. The
// bridge dial and herdr's blocked mapping come from the same ui_prompt
// events, so a dialog without the badge is a stale entry whose end
// event was lost - a crashed pi, not a waiting one. Cheap: no pane read.
func (a *app) dialogFor(sessionPath, badge string) (BridgeDialog, bool) {
	if badge != "blocked" {
		return BridgeDialog{}, false
	}
	return a.bridgeDialog(sessionPath)
}

type bridgeEntry struct {
	dialog  BridgeDialog
	expires time.Time
}

// apiBridgeDialog records or clears a dialog. Answers 204; the body is
// tiny and the extension retries nothing.
func (a *app) apiBridgeDialog(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phase       string `json:"phase"`
		Kind        string `json:"kind"`
		Title       string `json:"title"`
		ID          string `json:"id"`
		SessionPath string `json:"session_path"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.SessionPath == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	a.bridgeMu.Lock()
	if a.bridge == nil {
		a.bridge = map[string]bridgeEntry{}
	}
	if req.Phase == "start" {
		a.bridge[req.SessionPath] = bridgeEntry{
			dialog:  BridgeDialog{ID: req.ID, Kind: req.Kind, Title: req.Title},
			expires: time.Now().Add(bridgeTTL),
		}
	} else if req.ID == "" {
		// An id-less end comes from an older extension: clear regardless.
		delete(a.bridge, req.SessionPath)
	} else if e, ok := a.bridge[req.SessionPath]; ok && e.dialog.ID == req.ID {
		// A replaced dialog carries a new id, so a late end for the old
		// one must not clear the replacement.
		delete(a.bridge, req.SessionPath)
	}
	a.bridgeMu.Unlock()
	w.WriteHeader(http.StatusNoContent)
}

// bridgeDialog returns the open dialog for a session path, if any.
func (a *app) bridgeDialog(sessionPath string) (BridgeDialog, bool) {
	a.bridgeMu.Lock()
	defer a.bridgeMu.Unlock()
	e, ok := a.bridge[sessionPath]
	if !ok {
		return BridgeDialog{}, false
	}
	if time.Now().After(e.expires) {
		delete(a.bridge, sessionPath)
		return BridgeDialog{}, false
	}
	return e.dialog, true
}
