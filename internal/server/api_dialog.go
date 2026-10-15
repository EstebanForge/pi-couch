package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

// apiDialogAnswer answers a pi dialog from the phone. Confirm kinds
// take yes (enter) or no (down then enter - pi's confirm cursor starts
// on Yes, and escape would leave the dialog unanswered). Select kinds
// take an option's text: the pane is re-read, the option located among
// the live rows, the cursor walked down and VERIFIED on the target row
// before enter goes out - the herdr-web-ui verify-then-fire discipline,
// because a wrong selection answers every later turn silently. escape
// dismisses any dialog. The safety rails come from the same study: the
// bridge id must still match, and the pane must still show the dialog
// before any key goes out. Every refusal answers 409 changed in-band,
// and the client drops its buttons - the stream brings whatever is on
// screen now.
func (a *app) apiDialogAnswer(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var req struct {
		ID     string `json:"id"`
		Answer string `json:"answer"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.Answer == "" {
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
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	paneID, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}

	switch {
	case req.Answer == "yes" || req.Answer == "no":
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 20)
		if err != nil || !piConfirmOnScreen(pr.Text) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return
		}
		keys := []string{"enter"}
		if req.Answer == "no" {
			keys = []string{"down", "enter"}
		}
		a.answerSend(w, ctx, paneID, keys)

	case req.Answer == "escape":
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 20)
		if err != nil || !dialogOpen(pr.Text) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return
		}
		a.answerSend(w, ctx, paneID, []string{"esc"})

	default:
		a.answerSelect(w, r, ctx, paneID, req.Answer)
	}
}

// answerSelect walks pi's select cursor to the option and confirms it.
func (a *app) answerSelect(w http.ResponseWriter, r *http.Request, ctx context.Context, paneID, want string) {
	pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 20)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}
	m := parseMenu(pr.Text)
	if m == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}
	target := -1
	for i, o := range m.Options {
		if strings.TrimSpace(o) == strings.TrimSpace(want) {
			target = i
			break
		}
	}
	if target < 0 {
		// The card's options no longer match the screen.
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}
	if steps := aimSteps(m.Cursor, target, len(m.Options)); steps > 0 {
		downs := make([]string, steps)
		for i := range downs {
			downs[i] = "down"
		}
		if err := a.herdr.PaneSendInput(ctx, paneID, "", downs); err != nil {
			a.logger.Error("dialog answer", "err", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "send failed"})
			return
		}
		// Verify-then-fire: confirm the cursor really sits on the
		// target row before the enter. A vanished or moved menu means
		// the screen changed under us.
		pr2, err := a.herdr.PaneRead(ctx, paneID, "detection", 20)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return
		}
		if m2 := parseMenu(pr2.Text); m2 == nil || m2.Cursor != target {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return
		}
	}
	a.answerSend(w, ctx, paneID, []string{"enter"})
}

// answerSend pushes the keys and reports. Send failures keep the bar
// (only a network throw re-enables it client-side), so a 502 here is
// the one refusal the client retries.
func (a *app) answerSend(w http.ResponseWriter, ctx context.Context, paneID string, keys []string) {
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
