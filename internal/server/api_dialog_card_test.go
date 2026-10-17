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
