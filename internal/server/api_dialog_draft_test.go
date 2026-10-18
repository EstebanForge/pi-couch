package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"
)

// A scrolled editor only paints a window of the buffer, but the tool
// call behind the dialog carries the whole draft: the wire must serve
// the draft so the card prefills complete, with no fragment flag.
func TestDialogWireServesDraft(t *testing.T) {
	pane := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 4 more ────\n" +
		"body line three\nbody line four\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &pane})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	f, err := os.OpenFile(sessPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(`{"type":"message","id":"d1","message":{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"git_commit","arguments":{"subject":"feat: draft","body":"line one\nline two"}}]}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	a.awaiting = newAwaitingDetector(a.herdr, time.Hour)
	a.awaiting.tick(context.Background())
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	d, ok := a.dialogFor(sessPath, "blocked")
	if !ok || d.ID == "" {
		t.Fatalf("no dialog: %+v", d)
	}
	w := a.dialogWireFor(sessPath, d, ok)
	if w == nil || w.Scrolled {
		t.Fatalf("fragment wire: %+v", w)
	}
	if w.Body != "feat: draft\n\nline one\nline two" {
		t.Fatalf("body %q", w.Body)
	}
}

// Without a draft-shaped tool call the wire keeps the pane fragment
// flagged scrolled, so the card opens with the replace-the-buffer box.
func TestDialogWireKeepsFragmentWithoutDraft(t *testing.T) {
	pane := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 4 more ────\n" +
		"body line three\nbody line four\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &pane})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.awaiting = newAwaitingDetector(a.herdr, time.Hour)
	a.awaiting.tick(context.Background())
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	d, ok := a.dialogFor(sessPath, "blocked")
	if !ok || d.ID == "" {
		t.Fatalf("no dialog: %+v", d)
	}
	w := a.dialogWireFor(sessPath, d, ok)
	if w == nil || !w.Scrolled {
		t.Fatalf("wire must keep the fragment: %+v", w)
	}
}

// The backspace sweep must be sized by the full draft, not the painted
// fragment: a scrolled buffer's fragment is far shorter than the real
// buffer, and an under-sweep leaves junk that the typed text then
// appends to, corrupting pi's commit.
func TestDialogAnswerEditorScrolledSweepsFullDraft(t *testing.T) {
	pane := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 4 more ────\n" +
		"body line three\nbody line four\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	var keysLog [][]string
	lines := make([]string, 12)
	for i := range lines {
		lines[i] = fmt.Sprintf("description line %d with enough words to pad the buffer", i+1)
	}
	after := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 2 more ────\n" +
		strings.Join(lines, "\n") + "\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &pane,
		paneSeq: []string{after}, keysLog: &keysLog,
	})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	longBody := ""
	for i := 0; i < 12; i++ {
		longBody += lines[i] + "\n"
	}
	f, err := os.OpenFile(sessPath, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	args, _ := json.Marshal(map[string]string{"subject": "feat: long draft", "body": strings.TrimSpace(longBody)})
	if _, err := f.WriteString(`{"type":"message","id":"d1","message":{"role":"assistant","content":[{"type":"toolCall","id":"c1","name":"git_commit","arguments":` + string(args) + `}]}}` + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	full := "feat: long draft\n\n" + strings.TrimSpace(longBody)
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":`+strconv.Quote(full)+`}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}

	var bs int
	for _, k := range keysLog {
		if len(k) > 0 && k[0] == "backspace" {
			bs = len(k)
		}
	}
	draftRunes := utf8.RuneCountInString("feat: long draft\n\n" + strings.TrimSpace(longBody))
	if bs < draftRunes {
		t.Fatalf("sweep=%d backspaces, draft=%d runes - under-sweep would corrupt the buffer", bs, draftRunes)
	}
}
