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
	// Claim before any key goes out: a double-tap racing the pane
	// re-reads would send a second Enter, which answers whatever pi
	// shows next. Any outcome that did not complete a send releases
	// the claim so a corrected retry can proceed.
	if !a.answered.Claim(req.ID) {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}
	if !a.answerDispatch(w, r, req.Answer, meta.Path) {
		a.answered.Release(req.ID)
	}
}

// answerDispatch runs one answer attempt; true means keys were sent
// and the 200 already written.
func (a *app) answerDispatch(w http.ResponseWriter, r *http.Request, answer, path string) bool {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	paneID, err := a.herdr.TargetForPath(ctx, path)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false
	}

	switch {
	case answer == "yes" || answer == "no":
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil || !piConfirmOnScreen(pr.Text) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		}
		keys := []string{"enter"}
		if answer == "no" {
			keys = []string{"down", "enter"}
		}
		return a.answerSend(w, ctx, paneID, keys)

	case answer == "submit":
		// The editor overlay's accept key is enter (escape cancels in
		// the generic branch below). The screen must still be the
		// editor before the key goes out.
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		}
		if d := parsePiDialog(pr.Text); d == nil || d.Kind != "editor" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		}
		return a.answerSend(w, ctx, paneID, []string{"enter"})

	case answer == "escape":
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil || !dialogOpen(pr.Text) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		}
		return a.answerSend(w, ctx, paneID, []string{"esc"})

	default:
		return a.answerOption(w, r, ctx, paneID, answer)
	}
}

// answerOption walks pi's dialog cursor to the option and confirms it.
// The parse is the same one the card's rows came from (parsePiDialog),
// so a questionnaire's numbered rows, a plain menu and a confirm's
// Yes/No all answer through this path. The walk is settle-verified:
// pi repaints a beat after each key, so the cursor check retries
// instead of racing the redraw.
func (a *app) answerOption(w http.ResponseWriter, r *http.Request, ctx context.Context, paneID, want string) bool {
	var m *piDialog
	target := -1
	for attempt := 0; attempt < 3; attempt++ {
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		}
		m = parsePiDialog(pr.Text)
		if m == nil || len(m.Options) == 0 {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		}
		target = -1
		for i, o := range m.Options {
			if strings.TrimSpace(o.Label) == strings.TrimSpace(want) {
				target = i
				break
			}
		}
		if target < 0 {
			// The card's options no longer match the screen.
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		}
		if m.Cursor == target {
			break
		}
		steps := aimSteps(m.Cursor, target, len(m.Options))
		downs := make([]string, steps)
		for i := range downs {
			downs[i] = "down"
		}
		if err := a.herdr.PaneSendInput(ctx, paneID, "", downs); err != nil {
			a.logger.Error("dialog answer", "err", err)
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "send failed"})
			return false
		}
		select {
		case <-ctx.Done():
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		case <-time.After(250 * time.Millisecond):
		}
	}
	if m == nil || m.Cursor != target {
		// The cursor never settled on the target row.
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false
	}
	return a.answerSend(w, ctx, paneID, []string{"enter"})
}

// answerSend pushes the keys and reports. Send failures keep the bar
// (only a network throw re-enables it client-side), so a 502 here is
// the one refusal the client retries.
func (a *app) answerSend(w http.ResponseWriter, ctx context.Context, paneID string, keys []string) bool {
	if err := a.herdr.PaneSendInput(ctx, paneID, "", keys); err != nil {
		a.logger.Error("dialog answer", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "send failed"})
		return false
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return true
}

// piConfirmOnScreen reports whether pi's select/confirm menu hint is on
// the pane's visible screen: the re-read discipline before answering.
// The detection source is the visible screen only, so a buried,
// answered dialog in scrollback cannot satisfy it.
func piConfirmOnScreen(text string) bool {
	plain := ansiRe.ReplaceAllString(text, "")
	return piMenuHintRe.MatchString(plain)
}
