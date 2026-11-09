package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
)

// editorPaint builds pi's editor overlay paint with the given buffer
// body; an empty body is the emptied editor the answer flow's gate
// must see before it types.
func editorPaint(body string) string {
	return " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		body +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
}

// The dialog card's content comes from the pane parse, whatever kind
// the bridge reported: the rpiv questionnaire arrives as "custom" with
// an empty title, and only the pane knows what it asks.
func TestDialogWireCarriesParsedDialog(t *testing.T) {
	f := &fakePaneHerdr{
		agents: []hc.Agent{
			{Name: "a", AgentStatus: "blocked", PaneID: "p1", Session: &hc.AgentSessionRef{Value: "/s/a.jsonl"}},
		},
		panes: map[string]string{"p1": askUserScreen},
	}
	a := &app{logger: testLogger()}
	a.awaiting = newAwaitingDetector(f, time.Hour)
	a.awaiting.tick(context.Background())

	w := a.dialogWireFor("/s/a.jsonl", BridgeDialog{ID: "d1", Kind: "custom"}, true)
	if w == nil || w.ID != "d1" || w.Kind != "custom" {
		t.Fatalf("wire=%+v", w)
	}
	if w.Title != "Color" {
		t.Errorf("title=%q, want the pane's Color", w.Title)
	}
	if w.Body != "Which color, Red or Blue?" {
		t.Errorf("body=%q", w.Body)
	}
	if len(w.Options) != 3 || w.Options[0].Label != "Red" || w.Options[0].Description != "Pick red." || w.Cursor != 0 {
		t.Fatalf("options=%+v cursor=%d", w.Options, w.Cursor)
	}

	if a.dialogWireFor("/s/a.jsonl", BridgeDialog{}, false) != nil {
		t.Fatal("ok=false must carry no dialog")
	}

	// The wire type round-trips: status events carry the card fields.
	b, err := json.Marshal(w)
	if err != nil {
		t.Fatal(err)
	}
	var back map[string]any
	if err := json.Unmarshal(b, &back); err != nil {
		t.Fatalf("decode: %v", err)
	}
	for _, k := range []string{"id", "kind", "title", "body", "options", "cursor"} {
		if _, ok := back[k]; !ok {
			t.Errorf("wire json missing %s: %s", k, b)
		}
	}
}

// cardHCApp wires the hc dialog fixture: a fake core over the given
// paints (the detector's first tick consumes one), a ticked detector
// and a planted bridge entry.
func cardHCApp(t *testing.T, bridge BridgeDialog, paints ...string) (*app, *hcFakeCore, string) {
	t.Helper()
	a, sessPath := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath, paints...)
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	a.awaiting.tick(context.Background())
	if bridge.ID != "" {
		plantBridge(t, a, sessPath, bridge)
	}
	return a, f, sessPath
}

// postAnswer submits one dialog answer through the guarded routes.
func postAnswer(t *testing.T, a *app, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(body))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	return rec
}

// keyCounts folds the fake's key log into per-key totals.
func keyCounts(f *hcFakeCore) map[string]int {
	counts := map[string]int{}
	for _, call := range f.keyLog() {
		for _, k := range call {
			counts[k]++
		}
	}
	return counts
}

// A multi-question rpiv dialog keeps ONE dialog id across all its
// questions, so option answers must be repeatable per id - only
// confirms, escapes and editor submits are one-shot. The memo
// sequence below drives each answer against its own fixture.
func TestDialogAnswerMemoSequence(t *testing.T) {
	q1 := " ←  □ Slice  ■ Named  ✓ Submit →\n Which drink?\n❯ 1. Coffee\n     Hot.\n  2. Tea\n     Cold.\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	q2 := " ←  □ Slice  ■ Named  ✓ Submit →\n Which size?\n❯ 1. Big\n  2. Small\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	bridge := BridgeDialog{ID: "dmulti", Kind: "custom", Title: "Which drink?"}

	// Question 1.
	a, f, _ := cardHCApp(t, bridge, q1, q1, q1)
	if rec := postAnswer(t, a, `{"id":"dmulti","answer":"Coffee"}`); rec.Code != http.StatusOK {
		t.Fatalf("q1 answer code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := keyCounts(f)["enter"]; got != 1 {
		t.Fatalf("q1 enters=%d, want 1", got)
	}

	// Question 2 under the same id: the memo must release.
	a2, f2, _ := cardHCApp(t, bridge, q2, q2, q2)
	if rec := postAnswer(t, a2, `{"id":"dmulti","answer":"Big"}`); rec.Code != http.StatusOK {
		t.Fatalf("q2 answer code=%d body=%s", rec.Code, rec.Body.String())
	}
	// A re-answer of the same question is a harmless re-pick.
	if rec := postAnswer(t, a2, `{"id":"dmulti","answer":"Big"}`); rec.Code != http.StatusOK {
		t.Fatalf("re-answer code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := keyCounts(f2)["enter"]; got != 2 {
		t.Fatalf("q2 enters=%d, want one per answer", got)
	}
	if len(f2.sendLog()) != 0 {
		t.Fatal("option answers must never use terminal.send")
	}
}

// Answering a MID questionnaire question advances to the next
// question and must not touch the submit flow.
func TestDialogAnswerMultiMidQuestionNoSubmit(t *testing.T) {
	q1 := " ←  □ Drink  □ Size  ✓ Submit →\n Which drink?\n❯ 1. Coffee\n  2. Tea\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	q2 := " ←  ■ Drink  □ Size  ✓ Submit →\n Which size?\n❯ 1. Big\n  2. Small\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	a, f, _ := cardHCApp(t, BridgeDialog{ID: "dmid", Kind: "custom", Title: "Which drink?"}, q1, q1, q2, q2)
	rec := postAnswer(t, a, `{"id":"dmid","answer":"Coffee"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("q1 answer code=%d body=%s", rec.Code, rec.Body.String())
	}
	counts := keyCounts(f)
	if counts["enter"] != 1 || counts["space"] != 0 {
		t.Fatalf("keys=%v, want only the answering enter", counts)
	}
}

// Answering the LAST question parks pi on the review screen; the
// endpoint must press "Submit answers" itself or pi records a decline.
func TestDialogAnswerMultiAutoSubmits(t *testing.T) {
	q2 := " ←  ■ Drink  □ Size  ✓ Submit →\n Which size?\n❯ 1. Big\n  2. Small\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	small := " ←  ■ Drink  ■ Size  ✓ Submit →\n Which size?\n  1. Big\n❯ 2. Small\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	review := " ←  ■ Drink  ■ Size  ✓ Submit →\n Review your answers\n\n  ● Drink\n    → Coffee\n  ● Size\n    → Small\n\n" +
		" Ready to submit your answers?\n❯ 1. Submit answers\n  2. Cancel\n" +
		" Enter to select · ↑/↓ to navigate · n to add a note · Esc to cancel"
	a, f, _ := cardHCApp(t, BridgeDialog{ID: "dsub", Kind: "custom", Title: "Which size?"}, q2, q2, small, review, review)
	rec := postAnswer(t, a, `{"id":"dsub","answer":"Small"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("final answer code=%d body=%s", rec.Code, rec.Body.String())
	}
	counts := keyCounts(f)
	if counts["down"] != 1 || counts["enter"] != 2 {
		t.Fatalf("keys=%v, want one down, answering enter, review submit enter", counts)
	}
}

// The bridge entry is ephemeral (a 10-minute TTL, and the blocked
// badge flaps). The pane parse is the durable source: the card must
// survive a long-idle reopen with a synthesized stable id, and the
// answer endpoint must accept that id.
func TestDialogWireSurvivesBridgeExpiry(t *testing.T) {
	f := &fakePaneHerdr{
		agents: []hc.Agent{
			{Name: "a", AgentStatus: "blocked", PaneID: "p1", Session: &hc.AgentSessionRef{Value: "/s/a.jsonl"}},
		},
		panes: map[string]string{"p1": askUserScreen},
	}
	a := &app{logger: testLogger()}
	a.awaiting = newAwaitingDetector(f, time.Hour)
	a.awaiting.tick(context.Background())

	// No bridge entry at all; the badge gate stays satisfied by the parse.
	for _, badge := range []string{"blocked", ""} {
		d, ok := a.dialogFor("/s/a.jsonl", badge)
		if !ok {
			t.Fatalf("badge %q: dialogFor came up empty", badge)
		}
		if !strings.HasPrefix(d.ID, "pane-") {
			t.Errorf("badge %q: id=%q, want a pane- synthesized id", badge, d.ID)
		}
		if d.Title != "Color" || d.Kind != "select" {
			t.Errorf("badge %q: dialog=%+v", badge, d)
		}
	}
	if _, ok := a.dialogFor("/s/none.jsonl", "blocked"); ok {
		t.Error("unknown session must carry no dialog")
	}

	id := a.paneDialogID("/s/a.jsonl")
	if id != a.paneDialogID("/s/a.jsonl") {
		t.Error("synthesized id must be stable across ticks")
	}
	d, _ := a.dialogFor("/s/a.jsonl", "blocked")
	w := a.dialogWireFor("/s/a.jsonl", d, true)
	if w == nil || w.ID != id || w.Title != "Color" || len(w.Options) != 3 {
		t.Fatalf("wire=%+v", w)
	}
}

// An option tap answered by the synthesized pane id: no bridge entry
// exists (TTL expired), the pane verify still gates the keys.
func TestDialogAnswerPaneFallbackID(t *testing.T) {
	before := askUserScreen
	after := " Color\n Which color, Red or Blue?\n  1. Red\n     Pick red.\n❯ 2. Blue\n     Pick blue.\n  3. Type something.\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Esc to cancel · Ctrl+] to collapse"
	a, f, sessPath := cardHCApp(t, BridgeDialog{}, before, before, after, after, after)
	if a.paneDialogID(sessPath) == "" {
		t.Fatal("fixture: no synthesized pane id")
	}

	if rec := postAnswer(t, a, `{"id":"pane-bogus","answer":"Blue"}`); rec.Code != http.StatusConflict {
		t.Fatalf("wrong id code=%d body=%s", rec.Code, rec.Body.String())
	}
	if rec := postAnswer(t, a, `{"id":"`+a.paneDialogID(sessPath)+`","answer":"Blue"}`); rec.Code != http.StatusOK {
		t.Fatalf("pane id code=%d body=%s", rec.Code, rec.Body.String())
	}
	counts := keyCounts(f)
	if counts["down"] != 1 || counts["enter"] != 1 {
		t.Fatalf("keys=%v, want down+enter", counts)
	}
}

// An option tap on the questionnaire walks the numbered rows: the
// cursor starts on Red, Blue is one down, the re-read verifies the
// cursor moved, then enter confirms.
func TestDialogAnswerQuestionnaireOption(t *testing.T) {
	before := askUserScreen
	after := " Color\n Which color, Red or Blue?\n  1. Red\n     Pick red.\n❯ 2. Blue\n     Pick blue.\n  3. Type something.\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Esc to cancel · Ctrl+] to collapse"
	a, f, _ := cardHCApp(t, BridgeDialog{ID: "dq", Kind: "custom", Title: "Color"}, before, before, after, after, after)
	rec := postAnswer(t, a, `{"id":"dq","answer":"Blue"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	counts := keyCounts(f)
	if counts["down"] != 1 || counts["enter"] != 1 {
		t.Fatalf("keys=%v, want down+enter", counts)
	}
}

// A free-text answer types into the activated input row with one
// forced send (hc appends the Enter); blank text is refused.
func TestDialogAnswerFreeText(t *testing.T) {
	onType := " Color\n Which flavor?\n  1. Red\n     Pick red.\n  2. Blue\n     Pick blue.\n❯ 3.\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	inputEcho := " Color\n Which flavor?\n  1. Red\n     Pick red.\n  2. Blue\n     Pick blue.\n❯ 3. pizza\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	a, f, _ := cardHCApp(t, BridgeDialog{ID: "dt", Kind: "custom", Title: "Color"}, onType, onType, onType, inputEcho)

	if rec := postAnswer(t, a, `{"id":"dt","answer":"type","text":"  "}`); rec.Code != http.StatusBadRequest {
		t.Fatalf("blank text code=%d", rec.Code)
	}
	if rec := postAnswer(t, a, `{"id":"dt","answer":"type","text":"pizza"}`); rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	sends := f.sendLog()
	if len(sends) != 1 || sends[0]["text"] != "pizza" {
		t.Fatalf("sends=%v, want one pizza send (hc appends the enter)", sends)
	}
	if len(f.keyLog()) != 0 {
		t.Fatalf("keys=%v, want none (no separate submit enter)", f.keyLog())
	}
}

// A phone-width pane wraps a long draft across rows; the answer still
// goes out - the echo check only logs, and the comparison is
// whitespace-insensitive.
func TestDialogAnswerFreeTextWrappedEcho(t *testing.T) {
	onType := " Color\n Which flavor?\n  1. Red\n     Pick red.\n  2. Blue\n     Pick blue.\n❯ 3.\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	chiliEcho := " Color\n Which flavor?\n  1. Red\n     Pick red.\n  2. Blue\n     Pick blue.\n❯ 3. chili con\n   carne\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	a, _, _ := cardHCApp(t, BridgeDialog{ID: "dw", Kind: "custom", Title: "Color"}, onType, onType, onType, chiliEcho)
	rec := postAnswer(t, a, `{"id":"dw","answer":"type","text":"chili con  carne"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("wrapped code=%d body=%s", rec.Code, rec.Body.String())
	}
}

// Escape only needs an open dialog on the fresh read: stranding the
// card on an identity mismatch is worse than a redundant esc.
func TestDialogAnswerEscapeUntitledBridgeDialog(t *testing.T) {
	before := " Colors\n Choose colors\n❯ 1. [ ] Red\n  2. [ ] Green\n  Submit\n" +
		" Enter to select · ↑/↓ to navigate · space to toggle · Esc to cancel"
	a, f, _ := cardHCApp(t, BridgeDialog{ID: "bridge-uuid", Kind: "custom"}, before, before, before)
	rec := postAnswer(t, a, `{"id":"bridge-uuid","answer":"escape"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("escape code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := keyCounts(f)["esc"]; got != 1 {
		t.Fatalf("esc presses=%d, want 1", got)
	}
}

// The card was minted for one dialog; the pane now shows a different
// one. Esc must still go out.
func TestDialogAnswerEscapeDifferentDialogStillCancels(t *testing.T) {
	before := " Colors\n Choose colors\n❯ 1. [ ] Red\n  2. [ ] Green\n  Submit\n" +
		" Enter to select · ↑/↓ to navigate · space to toggle · Esc to cancel"
	other := " Sizes\n Pick a size\n❯ 1. Small\n  2. Large\n  Submit\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel"
	a, f, sessPath := cardHCApp(t, BridgeDialog{}, before, other, other, other)
	stale := paneParseID(sessPath, parsePiDialog(before))
	rec := postAnswer(t, a, `{"id":"`+stale+`","answer":"escape"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("escape code=%d body=%s", rec.Code, rec.Body.String())
	}
	if got := keyCounts(f)["esc"]; got != 1 {
		t.Fatalf("esc presses=%d, want 1", got)
	}
}

// Toggling walks the cursor to the row and presses space, over both
// verbs (a checkbox row reached as "option" routes to the toggle);
// rpiv has no number hotkeys, so digit sends would answer nothing.
func TestDialogAnswerToggleMultiSelect(t *testing.T) {
	for _, verb := range []string{"toggle", "option"} {
		t.Run(verb, func(t *testing.T) {
			before := " Colors\n Choose colors\n❯ 1. [ ] Red\n  Include red.\n  2. [ ] Green\n  Include green.\n  3. [ ] Type something.\n  Submit\n" +
				" Enter to select · ↑/↓ to navigate · space to toggle · Esc to cancel"
			a, f, _ := cardHCApp(t, BridgeDialog{ID: "dm", Kind: "custom", Title: "Colors"}, before, before, before)
			rec := postAnswer(t, a, `{"id":"dm","answer":"`+verb+`","text":"Red"}`)
			if rec.Code != http.StatusOK {
				t.Fatalf("toggle code=%d body=%s", rec.Code, rec.Body.String())
			}
			counts := keyCounts(f)
			if counts["space"] != 1 || counts["enter"] != 0 {
				t.Fatalf("keys=%v, want one space, no enter", counts)
			}
			if len(f.sendLog()) != 0 {
				t.Fatal("toggles must never use terminal.send")
			}
		})
	}
}

// A cursor two rows from the target walks down twice, then spaces.
func TestDialogAnswerToggleWalksAndSpaces(t *testing.T) {
	before := " Colors\n Choose colors\n❯ 1. [ ] Red\n  Include red.\n  2. [ ] Green\n  Include green.\n  3. [ ] Blue\n  Include blue.\n  4. [ ] Type something.\n  Submit\n" +
		" Enter to select · ↑/↓ to navigate · space to toggle · Esc to cancel"
	onBlue := strings.Replace(before, "❯ 1. [ ] Red", "  1. [ ] Red", 1)
	onBlue = strings.Replace(onBlue, "  3. [ ] Blue", "❯ 3. [ ] Blue", 1)
	a, f, _ := cardHCApp(t, BridgeDialog{ID: "dm", Kind: "custom", Title: "Colors"}, before, before, onBlue, onBlue)
	rec := postAnswer(t, a, `{"id":"dm","answer":"toggle","text":"Blue"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("toggle code=%d body=%s", rec.Code, rec.Body.String())
	}
	counts := keyCounts(f)
	if counts["down"] != 2 || counts["space"] != 1 {
		t.Fatalf("keys=%v, want two downs and a space", counts)
	}
}

// A mid-repaint read can parse nothing while pi redraws the freshly
// opened dialog; the option answer must settle and retry instead of
// refusing on the first blind read.
func TestDialogAnswerEntryRetriesMidRepaint(t *testing.T) {
	junk := "plain transcript lines\nnothing dialog shaped here\n"
	red := " Pick one\n\n→ red\n  blue\n\n↑↓ navigate  enter select  escape/ctrl+c cancel\n"
	a, f, _ := cardHCApp(t, BridgeDialog{ID: "sel", Kind: "select", Title: "Pick one"}, junk, junk, red, red)
	rec := postAnswer(t, a, `{"id":"sel","answer":"red"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	counts := keyCounts(f)
	if counts["enter"] != 1 || counts["down"] != 0 {
		t.Fatalf("keys=%v, want one enter from the settled read", counts)
	}
}

// Every entry read blind must refuse, not dereference a nil parse.
func TestDialogAnswerEntryAllBlindRefuses(t *testing.T) {
	prose := "plain transcript lines\nnothing dialog shaped here\n"
	a, f, _ := cardHCApp(t, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		prose, prose, prose, prose, prose, prose)
	rec := postAnswer(t, a, `{"id":"d9","answer":"type","text":"probe commit"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(f.keyLog()) != 0 || len(f.sendLog()) != 0 {
		t.Fatal("a refusal must send nothing")
	}
}

// Every dialog answer POST must leave exactly one wide event carrying
// the outcome status - the refusal gates stay silent by design, so
// this line is the only journal evidence of reach-versus-never.
func TestDialogAnswerLogsWideEvent(t *testing.T) {
	var buf syncBuffer
	a, _, _ := streamTestApp(t)
	a.logger = slog.New(&buf)

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer",
		strings.NewReader(`{"id":"whatever","answer":"yes"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d", rec.Code)
	}
	out := buf.String()
	if !strings.Contains(out, "dialog answer") || !strings.Contains(out, "status=409") {
		t.Fatalf("wide event missing for a 409: %q", out)
	}
}

type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (s *syncBuffer) Handle(_ context.Context, r slog.Record) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.buf.WriteString(r.Message)
	r.Attrs(func(a slog.Attr) bool {
		s.buf.WriteString(" " + a.Key + "=" + a.Value.String())
		return true
	})
	return nil
}
func (s *syncBuffer) Enabled(_ context.Context, _ slog.Level) bool { return true }
func (s *syncBuffer) WithAttrs(_ []slog.Attr) slog.Handler         { return s }
func (s *syncBuffer) WithGroup(_ string) slog.Handler              { return s }

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.buf.String()
}
