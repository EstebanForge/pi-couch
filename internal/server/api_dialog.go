package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"
)

// apiDialogAnswer answers a pi confirm dialog from the phone: Yes
// presses enter, No presses down then enter (pi's confirm cursor starts
// on Yes, and Escape would leave the dialog unanswered). The safety
// rails come from the herdr-web-ui study: the bridge id must still
// match, and the pane must still show pi's confirm hint before any key
// goes out. Every refusal answers 409 changed in-band, and the client
// drops its buttons - the stream brings whatever is on screen now.
func (a *app) apiDialogAnswer(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var req struct {
		ID     string `json:"id"`
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || (req.Answer != "yes" && req.Answer != "no") {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	dialog, has := a.bridgeDialog(meta.Path)
	if !has || dialog.ID == "" || dialog.ID != req.ID {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
	defer cancel()
	paneID, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}
	pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 8)
	if err != nil || !piConfirmOnScreen(pr.Text) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}
	keys := []string{"enter"}
	if req.Answer == "no" {
		keys = []string{"down", "enter"}
	}
	if err := a.herdr.PaneSendInput(ctx, paneID, "", keys); err != nil {
		a.logger.Error("dialog answer", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "send failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// piConfirmOnScreen reports whether pi's select/confirm menu hint is on
// the pane's visible screen: the re-read discipline before answering.
// The detection source is the visible screen only, so a buried,
// answered dialog in scrollback cannot satisfy it.
func piConfirmOnScreen(text string) bool {
	plain := ansiRe.ReplaceAllString(text, "")
	return piMenuHintRe.MatchString(plain)
}
