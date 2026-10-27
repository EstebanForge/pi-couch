package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// The dialog card's content comes from the pane parse, whatever kind
// the bridge reported: the rpiv questionnaire arrives as "custom" with
// an empty title, and only the pane knows what it asks.
func TestDialogWireCarriesParsedDialog(t *testing.T) {
	f := &fakePaneHerdr{
		agents: []herdr.Agent{
			{Name: "a", AgentStatus: "blocked", PaneID: "p1", Session: &herdr.AgentSessionRef{Value: "/s/a.jsonl"}},
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
		t.Fatal(err)
	}
	for _, k := range []string{"id", "kind", "title", "body", "options", "cursor"} {
		if _, ok := back[k]; !ok {
			t.Errorf("wire json missing %s: %s", k, b)
		}
	}
}

// Long messages must arrive at pi's editor in insertable chunks:
// a single text event over 10 lines or 1000 characters collapses
// into a [paste #n] marker (pi-tui paste guard) - the live bug that
// mangled a 13-line commit message into one.
func TestEditorChunks(t *testing.T) {
	short := "subject\n\nbody"
	if got := editorChunks(short); len(got) != 1 || got[0] != short {
		t.Fatalf("short: %q", got)
	}
	long := strings.Repeat("line\n", 12) + "end"
	got := editorChunks(long)
	if strings.Join(got, "") != long {
		t.Fatalf("chunks do not reassemble: %d chunks", len(got))
	}
	for i, c := range got {
		if n := strings.Count(c, "\n"); n > 10 {
			t.Fatalf("chunk %d carries %d newlines", i, n)
		}
		if len(c) > 1000 {
			t.Fatalf("chunk %d is %d bytes", i, len(c))
		}
	}
}

// The editor dialog answers in one step: even the no-edit submit is
// the type verb now - rewrite, verify, enter; a pane that no longer
// shows the editor is refused with no keys.
// After typing a long message the editor viewport only paints the
// tail plus its "↑ N more" rule. The verify must accept the painted
// tail as a suffix of the intended message - the old exact match
// 409'd a rewrite that had actually landed (the live bug).
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

func TestDialogAnswerEditorScrolledVerify(t *testing.T) {
	scrolled := editorPaint("──── ↑ 4 more ────\nbody line three\nbody line four\n")
	var sends int32
	var sessPath string
	var lastText string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, lastText: &lastText,
		paneText: &scrolled,
		paneSeq:  []string{scrolled, editorPaint("\n"), scrolled, scrolled},
	})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	want := "feat: subject\n\nbody line one\nbody line two\nbody line three\nbody line four"
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"feat: subject\n\nbody line one\nbody line two\nbody line three\nbody line four"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if lastText == "" {
		t.Fatalf("no text sent")
	}
	_ = want
}

// A multi-question rpiv dialog keeps ONE dialog id across all its
// questions, so option answers must be repeatable per id - only
// confirms, escapes and editor submits are one-shot. The second
// question's answer must pass; a re-answer of the same question is a
// harmless re-pick and must pass too.
func TestDialogAnswerMemoAllowsNextQuestion(t *testing.T) {
	q1 := " ←  □ Slice  ■ Named  ✓ Submit →\n Which drink?\n❯ 1. Coffee\n     Hot.\n  2. Tea\n     Cold.\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	q2 := " ←  □ Slice  ■ Named  ✓ Submit →\n Which size?\n❯ 1. Big\n  2. Small\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	var sends int32
	var sessPath string
	pane := q1
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &pane, paneAfterSend: &pane})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "dmulti", Kind: "custom", Title: "Which drink?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.guard(a.routes()).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(`{"id":"dmulti","answer":"Coffee"}`); code != http.StatusOK {
		t.Fatalf("q1 answer code=%d", code)
	}
	// pi advances the tab to the next question under the same dialog id.
	pane = q2
	if code := post(`{"id":"dmulti","answer":"Big"}`); code != http.StatusOK {
		t.Fatalf("q2 answer code=%d, want the memo to allow the next question", code)
	}
	if code := post(`{"id":"dmulti","answer":"Big"}`); code != http.StatusOK {
		t.Fatalf("re-answer code=%d, want option answers repeatable", code)
	}
	if n := atomic.LoadInt32(&sends); n != 3 {
		t.Fatalf("sends=%d, want one enter per answer", n)
	}
}

// Answering a MID questionnaire question advances to the next
// question and must not touch the submit flow.
func TestDialogAnswerMultiMidQuestionNoSubmit(t *testing.T) {
	q1 := " ←  □ Drink  □ Size  ✓ Submit →\n Which drink?\n❯ 1. Coffee\n  2. Tea\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	q2 := " ←  ■ Drink  □ Size  ✓ Submit →\n Which size?\n❯ 1. Big\n  2. Small\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Tab to switch questions · Esc to cancel · Ctrl+] to collapse"
	var sends int32
	var sessPath string
	post := q2
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &q1, paneAfterSend: &post})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "dmid", Kind: "custom", Title: "Which drink?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(`{"id":"dmid","answer":"Coffee"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("q1 answer code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 1 {
		t.Fatalf("sends=%d, want only the answering enter", n)
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
	var sends int32
	var sessPath string
	seq := []string{small, review, review, review}
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &q2, paneSeq: seq})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "dsub", Kind: "custom", Title: "Which size?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(`{"id":"dsub","answer":"Small"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("final answer code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 3 {
		t.Fatalf("sends=%d, want down + answering enter + review submit enter", n)
	}
}
func TestDialogAnswerEditorSubmit(t *testing.T) {
	editor := editorPaint("probe commit\n")
	var sends int32
	var sessPath string
	pane := editor
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &pane,
		paneSeq: []string{editor, editorPaint("\n"), editor, editor}})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	answer := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
			`{"id":"d9","answer":"type","text":"probe commit"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.guard(a.routes()).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := answer(); code != http.StatusOK {
		t.Fatalf("submit code=%d", code)
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want walk,backspaces,text,enter", n)
	}

	// The editor gone from the screen: refused, no keys.
	pane = "the commit landed; the pane shows transcript now"
	if code := answer(); code != http.StatusConflict {
		t.Fatalf("gone code=%d", code)
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends after refusal=%d", n)
	}
}

// The bridge entry is ephemeral (a 10-minute TTL, and herdr's blocked
// badge flaps). The pane parse is the durable source: the card must
// survive a long-idle reopen with a synthesized stable id, and the
// answer endpoint must accept that id.
func TestDialogWireSurvivesBridgeExpiry(t *testing.T) {
	f := &fakePaneHerdr{
		agents: []herdr.Agent{
			{Name: "a", AgentStatus: "blocked", PaneID: "p1", Session: &herdr.AgentSessionRef{Value: "/s/a.jsonl"}},
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
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &before, paneAfterSend: &after})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridgeMu.Unlock()
	// The synthesized id comes from the detector's parse; build the
	// detector over the fake socket and tick it deterministically
	// instead of racing a poll loop.
	a.awaiting = newAwaitingDetector(a.herdr, time.Hour)
	a.awaiting.tick(context.Background())

	post := func(id string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
			`{"id":"`+id+`","answer":"Blue"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.guard(a.routes()).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post("pane-bogus"); code != http.StatusConflict {
		t.Fatalf("wrong id code=%d", code)
	}
	if code := post(a.paneDialogID(sessPath)); code != http.StatusOK {
		t.Fatalf("pane id code=%d", code)
	}
	if n := atomic.LoadInt32(&sends); n != 2 {
		t.Fatalf("sends=%d, want down+enter", n)
	}
}

// An option tap on the questionnaire walks the numbered rows: the
// cursor starts on Red, Blue is one down, the re-read verifies the
// cursor moved, then enter confirms.
func TestDialogAnswerQuestionnaireOption(t *testing.T) {
	before := askUserScreen
	after := " Color\n Which color, Red or Blue?\n  1. Red\n     Pick red.\n❯ 2. Blue\n     Pick blue.\n  3. Type something.\n" +
		" Enter to select · ↑/↓ to navigate · n to add notes · Esc to cancel · Ctrl+] to collapse"
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &before, paneAfterSend: &after})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "dq", Kind: "custom", Title: "Color"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"dq","answer":"Blue"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	// one down to Blue, one enter to confirm
	if n := atomic.LoadInt32(&sends); n != 2 {
		t.Fatalf("sends=%d, want down+enter", n)
	}
}

// A free-text answer: the endpoint walks the questionnaire's cursor to
// its "Type something." row (rpiv enters input mode on arrival - no
// opening enter), clears any restored draft with ctrl+u, types the
// text and submits - four sends: downs, ctrl+u, text, enter. The pane
// states follow the sends: the input-mode paint with an empty draft,
// then the draft echoing the typed text.
func TestDialogAnswerFreeText(t *testing.T) {
	before := askUserScreen
	onType := " Color\n Which flavor?\n  1. Red\n     Pick red.\n  2. Blue\n     Pick blue.\n❯ 3.\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	inputEcho := " Color\n Which flavor?\n  1. Red\n     Pick red.\n  2. Blue\n     Pick blue.\n❯ 3. pizza\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	var sends int32
	var sessPath string
	var lastText string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &before, lastText: &lastText,
		paneSeq: []string{onType, onType, inputEcho},
	})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "dt", Kind: "custom", Title: "Color"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()
	a.awaiting = newAwaitingDetector(a.herdr, time.Hour)
	a.awaiting.tick(context.Background())

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.guard(a.routes()).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(`{"id":"dt","answer":"type","text":"  "}`); code != http.StatusBadRequest {
		t.Fatalf("blank text code=%d", code)
	}
	if code := post(`{"id":"dt","answer":"type","text":"pizza"}`); code != http.StatusOK {
		t.Fatalf("code=%d", code)
	}
	// downs (one send), ctrl+u (clear a restored draft), the text, the
	// final enter - no opening enter: rpiv activates input mode on
	// cursor arrival.
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want down,ctrl+u,text,enter", n)
	}
}

// A phone-width pane wraps a long draft across rows; the echo check
// must be whitespace-insensitive, not a raw contains.
func TestDialogAnswerFreeTextWrappedEcho(t *testing.T) {
	before := askUserScreen
	onType := " Color\n Which flavor?\n  1. Red\n     Pick red.\n  2. Blue\n     Pick blue.\n❯ 3.\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	chiliEcho := " Color\n Which flavor?\n  1. Red\n     Pick red.\n  2. Blue\n     Pick blue.\n❯ 3. chili con\n   carne\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &before,
		paneSeq: []string{onType, chiliEcho},
	})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "dw", Kind: "custom", Title: "Color"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()
	a.awaiting = newAwaitingDetector(a.herdr, time.Hour)
	a.awaiting.tick(context.Background())

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(`{"id":"dw","answer":"type","text":"chili con  carne"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("wrapped code=%d", rec.Code)
	}
}

// The editor dialog's buffer is editable from the phone: a type
// answer on an editor pane rewrites the buffer (park the cursor at
// the end, backspace the old text, type the replacement with real
// newlines) and verifies the repaint WITHOUT submitting - Submit
// stays its own answer, so the memo claim must be released after a
// successful edit.
func TestDialogAnswerEditorEdit(t *testing.T) {
	editor := editorPaint("probe commit\n")
	edited := editorPaint("new subject\n\nnew body\n")
	var sends int32
	var sessPath string
	var lastText string
	var keysLog [][]string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &editor, lastText: &lastText, keysLog: &keysLog,
		paneSeq: []string{edited, editorPaint("\n"), edited, edited},
	})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.guard(a.routes()).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(`{"id":"d9","answer":"type","text":"new subject\n\nnew body"}`); code != http.StatusOK {
		t.Fatalf("edit code=%d", code)
	}
	// walk (ctrl+e + downs + ctrl+e), backspaces, text, enter - one
	// step, like pi's own accept.
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want walk,backspaces,text,enter", n)
	}
	if lastText != "new subject\n\nnew body" {
		t.Fatalf("typed text %q", lastText)
	}
	if len(keysLog) != 3 {
		t.Fatalf("keys calls=%d, want walk,backspaces,enter: %v", len(keysLog), keysLog)
	}
	walk := keysLog[0]
	if walk[0] != "ctrl+e" || walk[len(walk)-1] != "ctrl+e" {
		t.Fatalf("walk must park the cursor with ctrl+e: %v", walk)
	}
	for _, k := range keysLog[1] {
		if k != "backspace" {
			t.Fatalf("second call must be the backspace sweep: %v", keysLog[1])
		}
	}
	if len(keysLog[2]) != 1 || keysLog[2][0] != "enter" {
		t.Fatalf("third call must be enter: %v", keysLog[2])
	}
}

// A mid-repaint read can parse nothing while pi redraws the freshly
// opened dialog; the answer must settle and retry instead of refusing
// on the first blind read.
func TestDialogAnswerEntryRetriesMidRepaint(t *testing.T) {
	editor := editorPaint("probe commit\n")
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends,
		readSeq: []string{
			"plain transcript lines\nnothing dialog shaped here\n",
			editor,
			editorPaint("\n"),
			editor,
		}})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"probe commit"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want walk,backspaces,text,enter", n)
	}
}

func TestDialogAnswerVerifyToleratesMidRepaint(t *testing.T) {
	editor := editorPaint("probe commit\n")
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends,
		readSeq: []string{
			editor,
			editorPaint("\n"),
			"plain transcript lines\nnothing dialog shaped here\n",
			"plain transcript lines\nnothing dialog shaped here\n",
			editor,
		}})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"probe commit"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want walk,backspaces,text,enter", n)
	}
}

// herdr's socket stalls for seconds under load, so a read can fail
// without the dialog going anywhere. The "!!err" readSeq marker makes
// the fake fail that read; the answer must ride it out at every
// classification point instead of refusing on one lost read.
func TestDialogAnswerEntryToleratesReadErrors(t *testing.T) {
	editor := editorPaint("probe commit\n")
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends,
		readSeq: []string{
			"!!err",
			"!!err",
			editor,
			editorPaint("\n"),
			editor,
		}})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"probe commit"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want walk,backspaces,text,enter", n)
	}
}

func TestDialogAnswerGateToleratesReadErrors(t *testing.T) {
	editor := editorPaint("probe commit\n")
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends,
		readSeq: []string{
			editor,
			"!!err",
			editorPaint("\n"),
			editor,
		}})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"probe commit"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want walk,backspaces,text,enter", n)
	}
}

func TestDialogAnswerVerifyToleratesReadErrors(t *testing.T) {
	editor := editorPaint("probe commit\n")
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends,
		readSeq: []string{
			editor,
			editorPaint("\n"),
			"!!err",
			"!!err",
			editor,
		}})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"probe commit"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want walk,backspaces,text,enter", n)
	}
}

// Every entry read blind must refuse, not dereference a nil parse.
func TestDialogAnswerEntryAllBlindRefuses(t *testing.T) {
	var sends int32
	var sessPath string
	prose := "plain transcript lines\nnothing dialog shaped here\n"
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends,
		readSeq: []string{prose, prose, prose, prose, prose}})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"probe commit"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 0 {
		t.Fatalf("sends=%d, want none", n)
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
