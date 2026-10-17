package server

import (
	"context"
	"path/filepath"
	"testing"
	"time"
)

// A scrolled editor only paints windows of the buffer. The reveal
// walks the cursor to the top (the viewport follows it), pages down,
// and stitches the windows into the full message for the card.
func TestDialogRevealStitches(t *testing.T) {
	tail := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 4 more ────\n" +
		"body line three\nbody line four\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	head := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"feat: subject\n\nbody line one\nbody line two\n" +
		"──── ↓ 2 more ────\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	next := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 2 more ────\n" +
		"body line three\nbody line four\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	pane := tail
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sessPath, sends: &sends, paneText: &pane,
		paneSeq: []string{head, next, next},
	})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	first := parsePiDialog(tail)
	got := a.revealEditorBuffer(context.Background(), "p1", first)
	want := "feat: subject\n\nbody line one\nbody line two\nbody line three\nbody line four"
	if got != want {
		t.Fatalf("stitched %q, want %q", got, want)
	}
}

// The wire serves the stitched buffer once the reveal landed and
// keeps the fragment flagged until then.
func TestDialogWireServesReveal(t *testing.T) {
	tail := " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 4 more ────\n" +
		"body line three\nbody line four\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	var sends int32
	var sessPath string
	pane := tail
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
		t.Fatalf("fragment wire: %+v", w)
	}

	full := "feat: subject\n\nbody line one\nbody line two\nbody line three\nbody line four"
	frag := a.awaiting.parsed(sessPath)
	a.reveals.done(revealKey(sessPath, frag), full)
	w = a.dialogWireFor(sessPath, d, ok)
	if w == nil || w.Scrolled || w.Body != full {
		t.Fatalf("stitched wire: %+v", w)
	}
}
