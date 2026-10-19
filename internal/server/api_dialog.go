package server

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"
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
		Text   string `json:"text"`
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
		// The bridge entry is the usual id source, but a dialog the
		// pane still shows is answerable through the synthesized pane
		// id even after the entry expired; the dispatch re-reads and
		// re-parses the pane before any key goes out.
		if req.ID == "" || req.ID != a.paneDialogID(meta.Path) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return
		}
	}
	if !a.answered.Claim(req.ID) {
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
	sent, oneShot := a.answerDispatch(w, r, req.Answer, req.Text, meta.Path)
	if !sent || !oneShot {
		a.answered.Release(req.ID)
	}
}

// answerDispatch runs one answer attempt; true means keys were sent
// and the 200 already written.
func (a *app) answerDispatch(w http.ResponseWriter, r *http.Request, answer, text, path string) (bool, bool) {
	ctx, cancel := context.WithTimeout(r.Context(), 8*time.Second)
	defer cancel()
	paneID, err := a.herdr.TargetForPath(ctx, path)
	if err != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false, false
	}

	switch {
	case answer == "yes" || answer == "no":
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil || !piConfirmOnScreen(pr.Text) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, true
		}
		keys := []string{"enter"}
		if answer == "no" {
			keys = []string{"down", "enter"}
		}
		return a.answerSend(w, ctx, paneID, keys), true

	case answer == "type":
		return a.answerFreeText(w, r, ctx, paneID, path, text)

	case answer == "escape":
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil || !dialogOpen(pr.Text) {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, true
		}
		return a.answerSend(w, ctx, paneID, []string{"esc"}), true

	default:
		return a.answerOption(w, r, ctx, paneID, answer), false
	}
}

// answerOption walks pi's dialog cursor to the option and confirms it.
// The parse is the same one the card's rows came from (parsePiDialog),
// so a questionnaire's numbered rows, a plain menu and a confirm's
// Yes/No all answer through this path. The walk is settle-verified:
// pi repaints a beat after each key, so the cursor check retries
// instead of racing the redraw.
func (a *app) answerOption(w http.ResponseWriter, r *http.Request, ctx context.Context, paneID, want string) bool {
	if !a.settleWalk(w, r, ctx, paneID, want) {
		return false
	}
	ok := a.answerSend(w, ctx, paneID, []string{"enter"})
	if ok {
		a.maybeAutoSubmit(w, r, ctx, paneID)
	}
	return ok
}

// maybeAutoSubmit finishes a multi-question rpiv questionnaire: pi
// ends on a "Ready to submit your answers?" review screen whose first
// row is "Submit answers" - answering the last question only parks
// there, and an unsubmitted questionnaire records a decline. The read
// right after the answer races pi's repaint, so any paint without the
// review row just means wait and read again.
func (a *app) maybeAutoSubmit(w http.ResponseWriter, r *http.Request, ctx context.Context, paneID string) {
	for attempt := 0; attempt < 4; attempt++ {
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil {
			return
		}
		m := parsePiDialog(pr.Text)
		if m == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(250 * time.Millisecond):
			}
			continue
		}
		if !m.MultiQuestion {
			// A single-question dialog closes on its own.
			return
		}
		for _, o := range m.Options {
			if strings.EqualFold(o.Label, "submit answers") {
				a.paneSend(w, ctx, paneID, "", []string{"enter"})
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

// settleWalk aims pi's dialog cursor at the option labeled want and
// leaves it there, re-reading and re-aiming while pi repaints (a beat
// per key). The cursor check is verify-then-fire: the enter only goes
// out from the caller, after the cursor really sits on the row.
func (a *app) settleWalk(w http.ResponseWriter, r *http.Request, ctx context.Context, paneID, want string) bool {
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
	return true
}

// answerFreeText answers pi's free-text widget: reach input mode (the
// screen may already be there, or the questionnaire still needs its
// "Type something." row selected), type the text, verify pi echoed it
// onto the input line, then submit with enter.
func (a *app) answerFreeText(w http.ResponseWriter, r *http.Request, ctx context.Context, paneID, sessionPath, text string) (bool, bool) {
	trimmed := strings.TrimSpace(text)
	if trimmed == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return false, false
	}
	for attempt := 0; attempt < 4; attempt++ {
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, false
		}
		m := parsePiDialog(pr.Text)
		if m == nil && editorPaneEmpty(pr.Text) {
			// An emptied editor parses nil (no buffer rows); a sweep
			// that emptied it must still be completable - type into the
			// empty buffer and submit.
			m = &piDialog{Kind: "editor"}
		}
		if m != nil && m.Kind == "editor" {
			// The editor buffer is editable in one step: rewrite and
			// submit, like pi's own accept.
			return a.editEditorSubmit(w, r, ctx, paneID, sessionPath, m, text), true
		}
		if m != nil && m.InputMode {
			// rpiv activates input mode the moment the cursor lands on
			// the free-text row - no enter to open it; enter submits.
			ok := a.typeAndSubmit(w, r, ctx, paneID, trimmed)
			if ok {
				a.maybeAutoSubmit(w, r, ctx, paneID)
			}
			return ok, false
		}
		if m != nil && m.Kind == "input" {
			// pi's native input widget.
			return a.typeAndSubmit(w, r, ctx, paneID, trimmed), false
		}
		if m == nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, false
		}
		own := -1
		for i := range m.Options {
			if piOwnAnswerRe.MatchString(m.Options[i].Label) {
				own = i
				break
			}
		}
		if own < 0 {
			// No free-text row and no input widget: nothing to type into.
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, false
		}
		if m.Cursor == own {
			// On the row yet not in input mode: the dialog changed under
			// us; refuse rather than guess.
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, false
		}
		steps := aimSteps(m.Cursor, own, len(m.Options))
		downs := make([]string, steps)
		for i := range downs {
			downs[i] = "down"
		}
		if !a.paneSend(w, ctx, paneID, "", downs) {
			return false, false
		}
		select {
		case <-ctx.Done():
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false, false
		case <-time.After(250 * time.Millisecond):
		}
	}
	// pi never showed its input line.
	writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
	return false, false
}

// typeAndSubmit types the answer into pi's input line and only presses
// enter once the pane echoes the text back; a swallowed keystroke must
// not submit an empty or partial answer.
func (a *app) typeAndSubmit(w http.ResponseWriter, r *http.Request, ctx context.Context, paneID, text string) bool {
	// pi's input mode ignores the newline binding, so a multi-line
	// answer would garble; collapse to one line first.
	text = strings.Join(strings.Fields(text), " ")
	for attempt := 0; attempt < 3; attempt++ {
		// ctrl+u clears any draft rpiv restored from an earlier visit to
		// the row, so a stale prefix can never ride into the submit.
		if !a.paneSend(w, ctx, paneID, "", []string{"ctrl+u"}) {
			return false
		}
		if !a.paneSend(w, ctx, paneID, text, nil) {
			return false
		}
		select {
		case <-ctx.Done():
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		case <-time.After(150 * time.Millisecond):
		}
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
			return false
		}
		if piEchoHit(pr.Text, text) {
			return a.answerSend(w, ctx, paneID, []string{"enter"})
		}
	}
	writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
	return false
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

// paneSend is answerSend without the response: intermediate steps of a
// multi-key answer report through the final step only.
func (a *app) paneSend(w http.ResponseWriter, ctx context.Context, paneID, text string, keys []string) bool {
	if err := a.herdr.PaneSendInput(ctx, paneID, text, keys); err != nil {
		a.logger.Error("dialog answer", "err", err)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "send failed"})
		return false
	}
	return true
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

// editEditorSubmit rewrites pi's editor buffer (the git commit
// preview) and submits in one step, like pi's own accept: park the
// cursor at the buffer end (ctrl+e, a generous down-walk, ctrl+e
// again - herdr has no end key), backspace the old content (no-ops
// once empty), prove the buffer empty, type the replacement - raw
// newlines insert real lines in pi's editor - verify the repaint,
// then enter. Nothing is typed until the buffer reads empty: junk
// under the typed text is how commits got corrupted, so a sweep
// that cannot empty the buffer refuses with 409 instead of typing.
func (a *app) editEditorSubmit(w http.ResponseWriter, r *http.Request, ctx context.Context, paneID, sessionPath string, cur *piDialog, text string) bool {
	want := editorBufferOf(strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n"))
	if want == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return false
	}
	refuse := func() bool {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "changed"})
		return false
	}
	// A scrolled pane's Body is only the painted window; the draft in
	// the session jsonl is the full pre-edit buffer. Trust it only
	// while the pane parse agrees with it - a stale or diverged draft
	// would size the sweep wrong in the other direction.
	sweep := utf8.RuneCountInString(cur.Body) + 32
	if a.idx != nil {
		if meta, ok := a.idx.ByPath(sessionPath); ok {
			if draft := a.idx.ToolCallDraft(meta.Key); draft != "" && editorBufferMatches(cur, draft) {
				if n := utf8.RuneCountInString(draft) + 32; n > sweep {
					sweep = n
				}
			}
		}
	}
	// A head-window paint hides the buffer's tail behind its "↓ N
	// more" rule; the down count must cover those rows or the sweep
	// deletes from mid-buffer.
	lines := strings.Count(cur.Body, "\n") + 1 + cur.HiddenBelow
	walk := make([]string, 0, lines+10)
	walk = append(walk, "ctrl+e")
	for i := 0; i < lines+8; i++ {
		walk = append(walk, "down")
	}
	walk = append(walk, "ctrl+e")
	bs := make([]string, sweep)
	for i := range bs {
		bs[i] = "backspace"
	}
	if !a.paneSend(w, ctx, paneID, "", walk) || !a.paneSend(w, ctx, paneID, "", bs) {
		return false
	}
	emptied := false
	prevLen, stale, blind := -1, 0, 0
	gateDeadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(gateDeadline) {
		select {
		case <-ctx.Done():
			return refuse()
		case <-time.After(200 * time.Millisecond):
		}
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil {
			return refuse()
		}
		if editorPaneEmpty(pr.Text) {
			emptied = true
			break
		}
		m := parsePiDialog(pr.Text)
		if m == nil || m.Kind != "editor" {
			// A read can land mid-repaint and parse nothing; only a pane
			// that stays unparseable means pi left the dialog.
			blind++
			if blind >= 3 {
				return refuse()
			}
			continue
		}
		blind = 0
		// A draft-sized sweep is hundreds of keystrokes; herdr is
		// still delivering them while we poll. Wait for the buffer to
		// stop shrinking - a plateau with content left is a failed
		// sweep, never a reason to blind-retype.
		n := utf8.RuneCountInString(editorBufferOf(m.Body))
		if n == prevLen {
			stale++
			if stale >= 2 {
				return refuse()
			}
		} else {
			stale, prevLen = 0, n
		}
	}
	if !emptied {
		return refuse()
	}
	for _, chunk := range editorChunks(want) {
		if !a.paneSend(w, ctx, paneID, chunk, nil) {
			return false
		}
	}
	// pi repaints after the burst; poll briefly instead of one fixed
	// wait so a slow paint cannot force a spurious refusal.
	for poll := 0; poll < 6; poll++ {
		select {
		case <-ctx.Done():
			return refuse()
		case <-time.After(150 * time.Millisecond):
		}
		pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
		if err != nil {
			return refuse()
		}
		m := parsePiDialog(pr.Text)
		if m == nil || m.Kind != "editor" {
			return refuse()
		}
		if editorBufferMatches(m, want) {
			return a.answerSend(w, ctx, paneID, []string{"enter"})
		}
	}
	return refuse()
}

// editorBufferOf normalizes a commit-message buffer for comparison:
// every line trimmed, trailing blank lines dropped.
func editorBufferOf(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	for len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return strings.Join(lines, "\n")
}

// editorBufferMatches compares the repaint against the intended
// message. A long buffer outgrows the editor viewport, so the pane
// then only paints the tail under its "↑ N more" rule. Painted rows
// are visual - a long line wraps at the pane width - so both sides
// are flattened before comparing; the hidden count is a sanity bound
// rather than an exact window, because wrapped rows shift the unit.
func editorBufferMatches(m *piDialog, want string) bool {
	flat := func(s string) string {
		return strings.Join(strings.Fields(s), "")
	}
	have := flat(editorBufferOf(m.Body))
	full := flat(editorBufferOf(want))
	if !m.Scrolled {
		return have == full
	}
	if len(have) > len(full) || !strings.HasSuffix(full, have) {
		return false
	}
	wantLines := strings.Split(editorBufferOf(want), "\n")
	return m.HiddenAbove >= 0 && m.HiddenAbove <= len(wantLines)+8
}

// editorChunks splits a commit message into sends pi's editor will
// insert verbatim: one text event over 10 lines or 1000 characters
// collapses into a "[paste #n]" marker (pi-tui editor paste guard).
func editorChunks(s string) []string {
	if strings.Count(s, "\n") < 9 && len(s) <= 1000 {
		return []string{s}
	}
	lines := strings.Split(s, "\n")
	var chunks []string
	var cur []string
	chars := 0
	flush := func() {
		if len(cur) == 0 {
			return
		}
		chunks = append(chunks, strings.Join(cur, "\n")+"\n")
		cur = nil
		chars = 0
	}
	for _, line := range lines {
		if len(cur) >= 9 || chars+len(line) >= 1000 {
			flush()
		}
		cur = append(cur, line)
		chars += len(line) + 1
	}
	// The last chunk keeps the message's own (missing) trailing newline.
	if len(cur) > 0 {
		chunks = append(chunks, strings.Join(cur, "\n"))
	}
	return chunks
}
