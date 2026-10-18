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
	"sync/atomic"
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
	var sends int32
	var sessPath string
	var keysLog [][]string
	lines := make([]string, 12)
	for i := range lines {
		lines[i] = fmt.Sprintf("description line %d with enough words to pad the buffer", i+1)
	}
	// The pre-answer pane shows the draft's tail window - the last 11
	// lines under the scroll rule - so the draft guard accepts it.
	pane := editorPaint("──── ↑ 3 more ────\n" + strings.Join(lines[1:], "\n") + "\n")
	after := editorPaint("──── ↑ 2 more ────\n" + strings.Join(lines, "\n") + "\n")
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &pane,
		paneSeq: []string{after, editorPaint("\n"), after, after}, keysLog: &keysLog,
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

// beta24's sweep verified AFTER typing: a failed sweep left junk in
// pi's buffer and the typed text appended to it. The gate now proves
// the buffer empty before a single character goes out; if the sweep
// cannot empty it, the answer 409s with no text sent.
func TestDialogAnswerEditorSweepGatesOnEmpty(t *testing.T) {
	pane := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 4 more ────\n" +
		"body line three\nbody line four\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	var lastText string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &pane,
		paneSeq: []string{pane, pane, pane}, lastText: &lastText,
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

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"feat: subject\n\nbody line one"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if lastText != "" {
		t.Fatalf("typed into an uncleared buffer: %q", lastText)
	}
}

// A head-window paint hides the buffer's tail behind its "↓ N more"
// rule, so the park walk's down count must include the hidden rows or
// the sweep deletes from mid-buffer.
func TestDialogAnswerEditorHeadWindowWalk(t *testing.T) {
	pane := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"feat: head window\n\nbody line one\nbody line two\n" +
		"──── ↓ 10 more ────\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	var keysLog [][]string
	empty := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	full := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"feat: x\n\nbody\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &pane,
		paneSeq: []string{empty, empty, full, full},
		keysLog: &keysLog,
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

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(
		`{"id":"d9","answer":"type","text":"feat: x\n\nbody"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(keysLog) == 0 {
		t.Fatal("no keys logged")
	}
	walk := keysLog[0]
	downs := 0
	for _, k := range walk {
		if k == "down" {
			downs++
		}
	}
	// painted lines = 4 (subject, blank, two body lines) + HiddenBelow 10.
	if downs != 4+10+8 {
		t.Fatalf("walk downs=%d, want %d (lines+hidden+slack)", downs, 4+10+8)
	}
}

// The parser can flake on one tick (a mid-repaint pane read); the
// wire must not drop the card's message over a single bad parse, or
// the phone sheet closes itself mid-answer.
func TestDialogWireLatchesBody(t *testing.T) {
	pane := editorPaint("feat: draft\n\nline one\nline two\n")
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &pane})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.awaiting = newAwaitingDetector(a.herdr, time.Hour)
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	a.awaiting.tick(context.Background())
	d, ok := a.dialogFor(sessPath, "blocked")
	w := a.dialogWireFor(sessPath, d, ok)
	if w == nil || w.Body == "" {
		t.Fatalf("no body on the good parse: %+v", w)
	}

	pane = "plain transcript text with no dialog on it"
	a.awaiting.tick(context.Background())
	d, ok = a.dialogFor(sessPath, "blocked")
	w = a.dialogWireFor(sessPath, d, ok)
	if w == nil || w.Body != "feat: draft\n\nline one\nline two" {
		t.Fatalf("latch lost over one bad parse: %+v", w)
	}

	a.bridgeMu.Lock()
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "other", Kind: "editor", Title: "Amend last commit with this message?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()
	d, ok = a.dialogFor(sessPath, "blocked")
	w = a.dialogWireFor(sessPath, d, ok)
	if w == nil || w.Body != "" {
		t.Fatalf("new dialog inherited a stale body: %+v", w)
	}
}

// A sweep that emptied the buffer but then refused (a flaked gate
// read) leaves pi's editor EMPTY; parsePiDialog reads that as nil and
// the answer must still complete from the emptied state.
func TestDialogAnswerEditorPreEmptied(t *testing.T) {
	empty := editorPaint("\n")
	typed := editorPaint("probe commit\n")
	var sends int32
	var sessPath string
	var lastText string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, lastText: &lastText,
		paneText: &empty,
		paneSeq:  []string{empty, empty, typed, typed},
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

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer",
		strings.NewReader(`{"id":"d9","answer":"type","text":"probe commit"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	if n := atomic.LoadInt32(&sends); n != 4 {
		t.Fatalf("sends=%d, want walk,backspaces,text,enter", n)
	}
	if lastText != "probe commit" {
		t.Fatalf("typed text %q", lastText)
	}
}

// The emptied editor surfaces on the wire as an answerable editor
// card with an empty body and a stable pane- id.
func TestDialogWireServesEmptyEditor(t *testing.T) {
	empty := editorPaint("\n")
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &empty})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	a.awaiting = newAwaitingDetector(a.herdr, time.Hour)

	a.awaiting.tick(context.Background())
	d, ok := a.dialogFor(sessPath, "blocked")
	if !ok || d.ID == "" {
		t.Fatalf("no dialog for the emptied editor")
	}
	w := a.dialogWireFor(sessPath, d, ok)
	if w == nil || w.Kind != "editor" || w.Body != "" {
		t.Fatalf("wire: %+v", w)
	}
	a.awaiting.tick(context.Background())
	d2, _ := a.dialogFor(sessPath, "blocked")
	if d2.ID != d.ID {
		t.Fatalf("id flipped: %s vs %s", d.ID, d2.ID)
	}
}
