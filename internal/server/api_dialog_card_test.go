package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
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

// The editor dialog answers submit with one enter; the screen must
// still be the editor when the key goes out.
func TestDialogAnswerEditorSubmit(t *testing.T) {
	editor := " Commit staged changes with this message?\n\nprobe commit\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	pane := editor
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &pane})
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
			`{"id":"d9","answer":"submit"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.guard(a.routes()).ServeHTTP(rec, req)
		return rec.Code
	}
	if code := answer(); code != http.StatusOK {
		t.Fatalf("submit code=%d", code)
	}
	if n := atomic.LoadInt32(&sends); n != 1 {
		t.Fatalf("sends=%d, want one enter", n)
	}

	// The editor gone from the screen: refused, no keys.
	pane = "the commit landed; the pane shows transcript now"
	if code := answer(); code != http.StatusConflict {
		t.Fatalf("gone code=%d", code)
	}
	if n := atomic.LoadInt32(&sends); n != 1 {
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
	editor := " Commit staged changes with this message?\n\nprobe commit\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	edited := " Commit staged changes with this message?\n\nnew subject\n\nnew body\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	var lastText string
	var lastKeys []string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &editor, lastText: &lastText, lastKeys: &lastKeys,
		paneSeq: []string{edited},
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
	// walk (ctrl+e + downs + ctrl+e), backspaces, text - no enter.
	if n := atomic.LoadInt32(&sends); n != 3 {
		t.Fatalf("sends=%d, want walk,backspaces,text", n)
	}
	if lastText != "new subject\n\nnew body" {
		t.Fatalf("typed text %q", lastText)
	}
	if len(lastKeys) < 8 {
		t.Fatalf("last keys call too short: %v", lastKeys)
	}
	for _, k := range lastKeys {
		if k != "backspace" {
			t.Fatalf("expected the backspace sweep last, got %v", lastKeys)
		}
		break
	}
	// The edit is non-terminal: Submit must still be claimable.
	if code := post(`{"id":"d9","answer":"submit"}`); code != http.StatusOK {
		t.Fatalf("submit after edit code=%d", code)
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends after submit=%d, want +enter", n)
	}
}
