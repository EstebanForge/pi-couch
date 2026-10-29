package server

import (
	"encoding/json"
	"hash/fnv"
	"net/http"
	"strconv"
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

// dialogFor prefers the bridge entry while herdr reports blocked, and
// falls back to the pane parse, which survives both the entry's TTL
// and badge flaps. The bridge entry alone never surfaces without the
// badge: both map the same ui_prompt events, so a dialog without the
// badge is a stale entry whose end event was lost - a crashed pi, not
// a waiting one.
func (a *app) dialogFor(sessionPath, badge string) (BridgeDialog, bool) {
	if badge == "blocked" {
		if d, ok := a.bridgeDialog(sessionPath); ok {
			return d, true
		}
	}
	// The bridge entry is ephemeral (TTL, and herdr's blocked badge
	// flaps mid-turn). The pane parse is the durable source: a dialog
	// on screen now is answerable now, registry or no registry.
	return a.paneDialog(sessionPath)
}

// paneDialogID derives a stable synthetic id from the parsed screen so
// answers can target a dialog the bridge never registered (or whose
// entry expired). Title and option labels only: the cursor moving
// must not mint a new identity mid-answer.
func (a *app) paneDialogID(sessionPath string) string {
	p := a.awaiting.parsed(sessionPath)
	if p == nil {
		return ""
	}
	return paneParseID(sessionPath, p)
}

// paneParseID is the pane-dialog identity used by the answer path's
// re-read: the same hash over a fresh parse must match the id the
// request targeted, or the pane shows a different dialog now.
func paneParseID(sessionPath string, p *piDialog) string {
	h := fnv.New64a()
	h.Write([]byte(sessionPath))
	h.Write([]byte{0x1f})
	h.Write([]byte(p.Title))
	for _, o := range p.Options {
		h.Write([]byte{0x1f})
		h.Write([]byte(o.Label))
	}
	return "pane-" + strconv.FormatUint(h.Sum64(), 36)
}

func (a *app) paneDialog(sessionPath string) (BridgeDialog, bool) {
	p := a.awaiting.parsed(sessionPath)
	if p == nil {
		return BridgeDialog{}, false
	}
	kind := "select"
	if p.Kind == "editor" {
		kind = "editor"
	}
	if p.Kind == "input" {
		kind = "input"
	}
	return BridgeDialog{ID: a.paneDialogID(sessionPath), Kind: kind, Title: p.Title}, true
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
	a.wakeDialog()
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
