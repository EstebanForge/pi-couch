package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
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
// statusWriter records the outcome status for the wide event; the
// JSON helpers write through untouched.
type statusWriter struct {
	http.ResponseWriter
	status int
}

func (s *statusWriter) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (a *app) apiDialogAnswer(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var req struct {
		ID     string `json:"id"`
		Answer string `json:"answer"`
		Text   string `json:"text"`
	}
	start := time.Now()
	sw := &statusWriter{ResponseWriter: w, status: http.StatusOK}
	w = sw
	defer func() {
		// The refusal gates log causes, but this wide event is the one
		// line that proves a request was received at all and what came
		// of it - without it an empty journal cannot separate a silent
		// refusal from a request that never arrived.
		a.logger.Info("dialog answer", "status", sw.status,
			"dur_ms", time.Since(start).Milliseconds(),
			"req_id", req.ID, "answer", req.Answer, "session", key)
	}()
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
		// The bridge entry is the usual id source, but a dialog the
		// pane still shows is answerable through the synthesized pane
		// id even after the entry expired; the dispatch re-reads and
		// re-parses the pane before any key goes out.
		if req.ID == "" || req.ID != a.paneDialogID(meta.Path) {
			// Esc dismisses whatever is open, so a stale id must not
			// strand a cancel; dispatch still refuses when no dialog
			// is on screen, protecting pi's idle editor.
			if !(req.Answer == "escape" && a.awaiting.parsed(meta.Path) != nil) {
				a.logger.Warn("dialog answer refused", "cause", "id gate",
					"session", meta.Path, "req_id", req.ID, "bridge_id", dialog.ID, "has_bridge", has,
					"pane_id_computed", a.paneDialogID(meta.Path), "pane_parsed", a.awaiting.parsed(meta.Path) != nil)
				writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
				return
			}
		}
	}
	// Identity target for the dispatch's re-read: a matched bridge
	// entry anchors on its title (the pane parse cannot mint bridge
	// uuids); a synthesized pane id anchors on itself.
	expectID, expectTitle := req.ID, ""
	if has && dialog.ID != "" && dialog.ID == req.ID {
		expectTitle = dialog.Title
	}
	if !a.answered.Claim(req.ID) {
		a.logger.Warn("dialog answer refused", "cause", "memo claimed", "session", meta.Path, "req_id", req.ID)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return
	}
	// Claim before any key goes out: a double-tap racing the pane
	// re-reads would send a second Enter, which answers whatever pi
	// shows next. Any outcome that did not complete a send releases
	// the claim so a corrected retry can proceed; option and free-text
	// answers release on success too, because a multi-question rpiv
	// dialog keeps one id across its questions - only confirms,
	// escapes and editor submits are one-shot per id.
	sent, oneShot := a.answerDispatch(w, r, req.Answer, req.Text, meta.Path, expectID, expectTitle)
	if !sent || !oneShot {
		a.answered.Release(req.ID)
	}
}

// answerDispatch runs one answer attempt; true means keys were sent
// and the 200 already written. expectID/expectTitle are the identity
// the pane must still show: yes/no and escape verify the fresh parse
// matches, not merely that some dialog is up. yes on an editor is the
// submit-as-is accept, content-checked against the card's copy.
//
// Two hc facts shape the type verb: terminal.send always appends Enter
// (no type-then-verify-then-submit, and an editor rewrite would
// early-submit a corrupted buffer), so the type verb answers only
// real input widgets and refuses editors; everything else keeps the
// verify-then-fire discipline.
func (a *app) answerDispatch(w http.ResponseWriter, r *http.Request, answer, text, path, expectID, expectTitle string) (bool, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()
	drv := a.hcDrv
	if drv == nil {
		// No driver: nothing can be verified or sent; refuse in-band.
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false, false
	}
	addr, err := a.hcDrv.TargetForPath(ctx, path)
	if err != nil {
		a.logger.Warn("dialog answer refused", "cause", "target unresolved", "session", path, "err", err.Error())
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false, false
	}
	// stillUp reports whether the re-read shows the targeted dialog.
	stillUp := func(screen string) bool {
		p := parsePiDialog(screen)
		if p == nil {
			return false
		}
		if expectTitle != "" {
			return p.Title == expectTitle
		}
		if strings.HasPrefix(expectID, "pane-") {
			return paneParseID(path, p) == expectID
		}
		// A bridge uuid has no pane-hash counterpart. Its registry
		// match above plus a live dialog here is the identity gate.
		return true
	}
	read := func() (string, bool) {
		screen, err := drv.ReadScreen(ctx, addr, 60)
		if err != nil {
			select {
			case <-ctx.Done():
				return "", false
			case <-time.After(300 * time.Millisecond):
			}
			screen, err = drv.ReadScreen(ctx, addr, 60)
			if err != nil {
				return "", false
			}
		}
		return screen, true
	}

	if answer == "option" || answer == "toggle" {
		if _, ok := toggleOption(a.awaiting.parsed(path), text); ok {
			return a.answerToggle(w, ctx, drv, addr, text), false
		}
	}

	switch {
	case answer == "yes" || answer == "no":
		screen, ok := read()
		if !ok || !stillUp(screen) {
			a.logger.Warn("dialog answer refused", "cause", "dialog off screen", "session", path)
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, true
		}
		// pi's editor accepts as-is: the buffer lives in pi, so yes is
		// one enter on the targeted editor. The unscrolled buffer is
		// verifiable, so the card's served copy must still match what
		// pi paints - a same-title successor dialog then refuses
		// instead of committing a buffer the user never saw. A scrolled
		// editor paints only the tail, so it rests on the identity gate.
		// hc's send appends Enter and cannot rewrite a buffer, so no
		// stays refused here.
		if m := parsePiDialog(screen); m != nil && m.Kind == "editor" {
			if answer != "yes" {
				a.logger.Warn("dialog answer refused", "cause", "editor has no no", "session", path)
				writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
				return false, true
			}
			if !m.Scrolled {
				served := strings.Join(strings.Fields(text), " ")
				painted := strings.Join(strings.Fields(m.Body), " ")
				if served == "" || served != painted {
					a.logger.Warn("dialog answer refused", "cause", "editor buffer moved on", "session", path)
					writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
					return false, true
				}
			}
			return a.answerSend(w, ctx, drv, addr, "", []string{"enter"}), true
		}
		if !piConfirmOnScreen(screen) {
			a.logger.Warn("dialog answer refused", "cause", "confirm off screen", "session", path)
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, true
		}
		keys := []string{"enter"}
		if answer == "no" {
			keys = []string{"down", "enter"}
		}
		return a.answerSend(w, ctx, drv, addr, "", keys), true

	case answer == "type":
		return a.answerFreeText(w, ctx, drv, addr, path, text)

	case answer == "toggle":
		return a.answerToggle(w, ctx, drv, addr, text), false

	case answer == "escape":
		screen, ok := read()
		// Esc dismisses whatever dialog is open; stranding the card on
		// an identity mismatch is worse than a redundant esc. Only a
		// dialog-less screen refuses, protecting pi's idle editor.
		if !ok || !dialogOpen(screen) {
			a.logger.Warn("dialog answer refused", "cause", "dialog off screen", "session", path)
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, true
		}
		return a.answerSend(w, ctx, drv, addr, "", []string{"esc"}), true

	default:
		return a.answerOption(w, ctx, drv, addr, answer, true), false
	}
}

func toggleOption(m *piDialog, want string) (piDialogOption, bool) {
	if m == nil || m.InputMode {
		return piDialogOption{}, false
	}
	for _, o := range m.Options {
		if o.Checkbox && o.Number > 0 && strings.TrimSpace(o.Label) == strings.TrimSpace(want) {
			return o, true
		}
	}
	return piDialogOption{}, false
}

// answerSend pushes one text-or-keys step and reports. Key presses and
// forced sends keep the bar on failure (502), the one refusal the
// client retries.
func (a *app) answerSend(w http.ResponseWriter, ctx context.Context, drv *hc.Driver, addr, text string, keys []string) bool {
	var err error
	if text != "" {
		err = drv.TypeText(ctx, addr, text)
	} else {
		err = drv.PressKeys(ctx, addr, keys)
	}
	if err != nil {
		a.logger.Error("dialog answer", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "send failed"})
		return false
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	return true
}

// answerFreeText answers input widgets with one forced send (hc
// appends the Enter) and refuses editors: a chunked editor rewrite
// would early-submit a corrupted buffer, and the send's single big
// paste trips pi's paste guard. The post-send echo check only logs -
// the answer already went out.
func (a *app) answerFreeText(w http.ResponseWriter, ctx context.Context, drv *hc.Driver, addr, sessionPath, text string) (bool, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return false, false
	}
	var m *piDialog
	for settle := 0; settle < 4; settle++ {
		screen, err := drv.ReadScreen(ctx, addr, 60)
		if err != nil {
			select {
			case <-ctx.Done():
				writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
				return false, false
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		m = parsePiDialog(screen)
		if m == nil && editorPaneEmpty(screen) {
			// An emptied editor parses nil; it is unanswerable, and
			// typing into it would only paint the buffer.
			m = &piDialog{Kind: "editor"}
		}
		if m != nil {
			break
		}
		select {
		case <-ctx.Done():
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, false
		case <-time.After(250 * time.Millisecond):
		}
	}
	if m == nil {
		a.logger.Warn("dialog answer refused", "cause", "entry unparseable", "session", sessionPath)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false, false
	}
	if m.Kind == "editor" {
		a.logger.Warn("dialog answer refused", "cause", "editor unsupported", "session", sessionPath)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false, false
	}
	if !m.InputMode && m.Kind != "input" {
		// A questionnaire row that needs selecting first: hc's send
		// carries its own Enter, so typing here would answer the row
		// selection, not fill the input. Refuse rather than guess.
		a.logger.Warn("dialog answer refused", "cause", "input not active", "session", sessionPath)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false, false
	}
	one := strings.Join(strings.Fields(trimmed), " ")
	ok := a.answerSend(w, ctx, drv, addr, one, nil)
	if ok {
		if screen, err := drv.ReadScreen(ctx, addr, 60); err == nil && !piEchoHit(screen, one) {
			a.logger.Warn("typed answer echo missing", "session", sessionPath)
		}
	}
	return ok, false
}

func (a *app) answerToggle(w http.ResponseWriter, ctx context.Context, drv *hc.Driver, addr, want string) bool {
	// The option walk with submit=false is the toggle: aim, space, and
	// no auto-submit; the dispatch stillUp gate bounds drift.
	return a.answerOption(w, ctx, drv, addr, want, false)
}

// answerOption walks pi's dialog cursor to the option and confirms it,
// settle-verified: re-read, aim, verify the cursor, then fire.
func (a *app) answerOption(w http.ResponseWriter, ctx context.Context, drv *hc.Driver, addr, want string, submit bool) bool {
	var m *piDialog
	target := -1
	for attempt := 0; attempt < 3; attempt++ {
		screen, err := drv.ReadScreen(ctx, addr, 60)
		if err != nil {
			select {
			case <-ctx.Done():
				a.logger.Warn("dialog answer refused", "cause", "settle ctx done", "addr", addr)
				writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
				return false
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		m = parsePiDialog(screen)
		if m == nil {
			// A mid-repaint screen parses nothing; settle and re-read
			// within the same attempt budget instead of refusing.
			select {
			case <-ctx.Done():
				writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
				return false
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		if len(m.Options) == 0 {
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
		if err := drv.PressKeys(ctx, addr, downs); err != nil {
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
	if m != nil && m.Cursor != target {
		screen, err := drv.ReadScreen(ctx, addr, 60)
		if err == nil {
			m = parsePiDialog(screen)
		}
	}
	if m == nil || m.Cursor != target {
		a.logger.Warn("dialog answer refused", "cause", "cursor never settled", "option", want)
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false
	}
	key := "enter"
	if !submit {
		key = "space"
	}
	ok := a.answerSend(w, ctx, drv, addr, "", []string{key})
	if ok && submit {
		a.maybeAutoSubmit(w, ctx, drv, addr)
	}
	return ok
}

// maybeAutoSubmit finishes a multi-question rpiv questionnaire: pi
// ends on a "Ready to submit your answers?" review screen whose first
// row is "Submit answers" - answering the last question only parks
// there, and an unsubmitted questionnaire records a decline.
func (a *app) maybeAutoSubmit(_ http.ResponseWriter, ctx context.Context, drv *hc.Driver, addr string) {
	for attempt := 0; attempt < 4; attempt++ {
		screen, err := drv.ReadScreen(ctx, addr, 60)
		if err != nil {
			return
		}
		m := parsePiDialog(screen)
		if m == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		if !m.MultiQuestion {
			return
		}
		for _, o := range m.Options {
			if strings.EqualFold(o.Label, "submit answers") {
				_ = drv.PressKeys(ctx, addr, []string{"enter"})
				return
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(250 * time.Millisecond):
		}
	}
}

// piEchoHit reports whether the pane echoes the typed answer. The
// comparison ignores all whitespace (NBSP included): pi paints the
// cursor as an NBSP cell and a phone-width pane wraps long drafts
// across rows, so a raw Contains misses both.
func piEchoHit(pane, want string) bool {
	flat := func(s string) string {
		s = ansiRe.ReplaceAllString(s, "")
		s = strings.Join(strings.Fields(s), "")
		return s
	}
	return strings.Contains(flat(pane), flat(want))
}

// piConfirmOnScreen reports whether pi's select/confirm menu hint is on
// the visible screen: the re-read discipline before answering. The
// screen read is the visible surface only, so a buried, answered
// dialog in scrollback cannot satisfy it.
func piConfirmOnScreen(text string) bool {
	plain := ansiRe.ReplaceAllString(text, "")
	return piMenuHintRe.MatchString(plain)
}
