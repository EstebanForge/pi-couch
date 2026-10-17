package server

import (
	"os"
	"testing"
)

// The fixtures come from live pi panes probed 2026-10-17 (probe agent
// in a throwaway workspace, closed after): the rpiv ask-user
// questionnaire and the pi-git-me commit editor. herdr-web-ui's
// parsePiDialog (server/prompt.ts) is the parsing discipline these
// tests pin: hint at the end above pi's footer, rows above the hint,
// title and body above the blank.

const askUserScreen = ` Color
 Which color, Red or Blue?
❯ 1. Red
     Pick red.
  2. Blue
     Pick blue.
  3. Type something.
 Enter to select · ↑/↓ to navigate · n to add notes · Esc to cancel · Ctrl+] to collapse
~/.cache/probe-ui (main)
↑59k ↓367 $0.009 5.9%/1.0M (auto)   (zai) glm-5.3-flash · high`

const confirmScreen = ` Allow edit?

❯ Yes
  No
↑↓ navigate  enter select  escape/ctrl+c cancel
~/proj
55.5k/1.0M tokens`

const plainMenuScreen = `→ Alpha
  Beta
  Gamma
↑↓ navigate  enter select  escape/ctrl+c cancel
~/proj
55.5k/1.0M tokens`

func TestParsePiDialogAskUser(t *testing.T) {
	d := parsePiDialog(askUserScreen)
	if d == nil {
		t.Fatal("ask-user screen must parse")
	}
	if d.Kind != "select" {
		t.Errorf("kind=%q, want select", d.Kind)
	}
	if d.Title != "Color" {
		t.Errorf("title=%q", d.Title)
	}
	if d.Body != "Which color, Red or Blue?" {
		t.Errorf("body=%q", d.Body)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options=%d, want 3", len(d.Options))
	}
	want := []struct{ label, desc string }{
		{"Red", "Pick red."},
		{"Blue", "Pick blue."},
		{"Type something.", ""},
	}
	for i, w := range want {
		if d.Options[i].Label != w.label || d.Options[i].Description != w.desc {
			t.Errorf("option %d = %+v, want %q/%q", i, d.Options[i], w.label, w.desc)
		}
	}
	if d.Cursor != 0 {
		t.Errorf("cursor=%d", d.Cursor)
	}
}

func TestParsePiDialogEditorFromLiveCapture(t *testing.T) {
	raw, err := os.ReadFile("/tmp/editor-screen.txt")
	if err != nil {
		t.Skipf("live capture not present outside the probe machine: %v", err)
	}
	d := parsePiDialog(string(raw))
	if d == nil {
		t.Fatal("editor screen must parse")
	}
	if d.Kind != "editor" {
		t.Errorf("kind=%q, want editor", d.Kind)
	}
	if d.Title != "Commit staged changes with this message?" {
		t.Errorf("title=%q", d.Title)
	}
	if d.Body != "probe commit" {
		t.Errorf("body=%q", d.Body)
	}
	if len(d.Options) != 0 {
		t.Errorf("options=%d, want none", len(d.Options))
	}
}

func TestParsePiDialogEditor(t *testing.T) {
	screen := "──────────────────────────────────────────\n" +
		" Commit staged changes with this message?\n" +
		"──────────────────────────────────────────\n" +
		"probe commit\n" +
		"second line\n" +
		"──────────────────────────────────────────\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor\n" +
		"──────────────────────────────────────────\n" +
		"~/.cache/probe-ui (main)\n" +
		"↑189k ↓825 $0.030 6.0%/1.0M (auto)   (zai) glm-5.3-flash · high\n"
	d := parsePiDialog(screen)
	if d == nil {
		t.Fatal("editor screen must parse")
	}
	if d.Kind != "editor" {
		t.Errorf("kind=%q, want editor", d.Kind)
	}
	if d.Title != "Commit staged changes with this message?" {
		t.Errorf("title=%q", d.Title)
	}
	if d.Body != "probe commit\nsecond line" {
		t.Errorf("body=%q", d.Body)
	}
}

func TestParsePiDialogPlainMenuAndConfirm(t *testing.T) {
	m := parsePiDialog(plainMenuScreen)
	if m == nil || m.Kind != "select" || len(m.Options) != 3 ||
		m.Options[0].Label != "Alpha" || m.Cursor != 0 {
		t.Fatalf("menu = %+v", m)
	}
	c := parsePiDialog(confirmScreen)
	if c == nil || c.Kind != "select" || len(c.Options) != 2 ||
		c.Options[0].Label != "Yes" || c.Options[1].Label != "No" {
		t.Fatalf("confirm = %+v", c)
	}
	if c.Title != "Allow edit?" {
		t.Errorf("title=%q", c.Title)
	}
}

func TestParsePiDialogRefusals(t *testing.T) {
	cases := map[string]string{
		"model picker hint must not parse": "  Enter to select · Ctrl+S to set as default · Escape/Ctrl+C to cancel\n~/.cache/probe-ui\n55.5k/1.0M tokens\n",
		"plain transcript":                 "The agent replies with prose.\nNo hint here.\n",
		"empty":                            "",
	}
	for name, screen := range cases {
		if d := parsePiDialog(screen); d != nil {
			t.Errorf("%s: parsed %+v", name, d)
		}
	}
}

func TestParsePiDialogWrappedHint(t *testing.T) {
	// A phone-width pane wraps the hint; pi's footer follows it.
	screen := "→ Alpha\n  Beta\n↑↓ navigate  enter select\nescape/ctrl+c cancel\n~/proj\n55.5k/1.0M tokens\n"
	d := parsePiDialog(screen)
	if d == nil || len(d.Options) != 2 {
		t.Fatalf("wrapped hint must still parse: %+v", d)
	}
}

// pi's input mode (a native ctx.ui input, or the questionnaire after
// its "Type something." row): question head above, optionally a >-
// led draft line, input hint at the end. No rows, no cursor - a
// distinct kind, so answer paths never mistake it for a menu.
func TestParsePiDialogInputMode(t *testing.T) {
	screen := " Color\n Which flavor?\n\n > pizza_\n enter submit  escape/ctrl+c cancel\n~/.cache (main)\n↑59k ↓367 (zai) glm"
	d := parsePiDialog(screen)
	if d == nil || d.Kind != "input" {
		t.Fatalf("input screen must parse as input: %+v", d)
	}
	if d.Title != "Color" {
		t.Errorf("title=%q, want Color", d.Title)
	}
	if d.Body != "Which flavor?" {
		t.Errorf("body=%q, want the question without the > draft line", d.Body)
	}
	if len(d.Options) != 0 {
		t.Errorf("options=%+v, input has no rows", d.Options)
	}

	bare := " enter submit  escape/ctrl+c cancel\n"
	if d := parsePiDialog(bare); d == nil || d.Kind != "input" || d.Title != "" {
		t.Errorf("bare input line must still parse: %+v", d)
	}
}

// pi's questionnaire in input mode (the cursor reached the "Type
// something." row): the row's label is replaced by the live draft and
// the hint gains newline/clear segments. Captured live 2026-10-17 on
// the probe-ft agent; the answer flow's phase transitions key on this
// paint, and the wire must keep the options while the user types.
func TestParsePiDialogAskInputMode(t *testing.T) {
	empty := " Drink\n Coffee or tea?\n\n  1. Coffee\n     Coffee\n  2. Tea\n     Tea\n❯ 3.\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear\n" +
		"~/.cache/probe-ft"
	d := parsePiDialog(empty)
	if d == nil || !d.InputMode {
		t.Fatalf("empty-draft input paint must parse with InputMode: %+v", d)
	}
	if d.Title != "Drink" {
		t.Errorf("title=%q", d.Title)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options=%d, want 3", len(d.Options))
	}
	if d.Cursor != 2 {
		t.Errorf("cursor=%d, want 2", d.Cursor)
	}

	typed := " Drink\n Coffee or tea?\n\n  1. Coffee\n     Coffee\n  2. Tea\n     Tea\n❯ 3. mint\n" +
		" Enter to select · ↑/↓ to navigate · Esc to cancel · Ctrl+] to collapse · Shift+Enter for newline · Ctrl+U to clear"
	d = parsePiDialog(typed)
	if d == nil || !d.InputMode || d.Cursor != 2 {
		t.Fatalf("typed paint must parse with InputMode: %+v", d)
	}
	if len(d.Options) != 3 || d.Options[2].Label != "mint" {
		t.Errorf("draft row must carry the typed text: %+v", d.Options)
	}
}

// The multi-select resting hint interleaves "Space to toggle" between
// navigate and notes (rpiv's buildHintText); the loose match must
// still anchor, and the input-mode paint must win over it.
func TestParsePiDialogMultiSelectHint(t *testing.T) {
	screen := " Pick\n Toppings?\n\n❯ 1. Cheese\n     Cheese\n  2. Ham\n     Ham\n  3. Type something.\n" +
		" Enter to select · ↑/↓ to navigate · Space to toggle · n to add notes · Esc to cancel · Ctrl+] to collapse"
	d := parsePiDialog(screen)
	if d == nil || d.Kind != "select" || d.InputMode {
		t.Fatalf("multi resting hint must parse as resting select: %+v", d)
	}
	if len(d.Options) != 3 {
		t.Fatalf("options=%d, want 3", len(d.Options))
	}
}

// A commit message's blank subject/body separator lives INSIDE the
// buffer: the whole text between pi's divider rules is the buffer,
// and only the rules bound it - an internal blank must survive into
// Body, with Title staying the overlay's question.
// A long buffer scrolls: pi paints the visible tail plus a rule
// carrying "↑ N more". The parse must flag the clip, not swallow the
// rule as message content - the card prefills from this body.
// The head window of a scrolled buffer hides its tail instead: pi
// paints a "↓ N more" rule at the viewport's bottom edge. The
// reveal scanner pages on this signal.
func TestParsePiDialogEditorScrollDown(t *testing.T) {
	screen := "some transcript line\n\n" +
		"────────────────────────────────────────────────────────────\n" +
		" Commit staged changes with this message?\n" +
		"────────────────────────────────────────────────────────────\n" +
		"feat: subject\n\nbody line one\n" +
		"────── ↓ 5 more ──────\n" +
		"────────────────────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	d := parsePiDialog(screen)
	if d == nil || d.Kind != "editor" {
		t.Fatalf("parse: %+v", d)
	}
	if !d.Scrolled || d.HiddenAbove != 0 || d.HiddenBelow != 5 {
		t.Fatalf("scrolled=%v above=%d below=%d", d.Scrolled, d.HiddenAbove, d.HiddenBelow)
	}
	if d.HiddenBelow != 5 {
		t.Fatalf("hidden below=%d", d.HiddenBelow)
	}
	if want := "feat: subject\n\nbody line one"; d.Body != want {
		t.Fatalf("body=%q, want %q", d.Body, want)
	}
}

func TestParsePiDialogEditorScrolled(t *testing.T) {
	screen := "some transcript line\n\n" +
		"────────────────────────────────────────────────────────────\n" +
		" Commit staged changes with this message?\n" +
		"────────────────────────────────────────────────────────────\n" +
		"────── ↑ 4 more ──────\n" +
		"the visible tail\nlines two\n" +
		"────────────────────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	d := parsePiDialog(screen)
	if d == nil || d.Kind != "editor" {
		t.Fatalf("parse: %+v", d)
	}
	if !d.Scrolled || d.HiddenAbove != 4 {
		t.Fatalf("scrolled=%v hidden=%d", d.Scrolled, d.HiddenAbove)
	}
	if want := "the visible tail\nlines two"; d.Body != want {
		t.Fatalf("body=%q, want %q", d.Body, want)
	}
}

func TestParsePiDialogEditorBufferKeepsBlank(t *testing.T) {
	screen := "some transcript line\n\n" +
		"────────────────────────────────────────────────────────────\n" +
		" Commit staged changes with this message?\n" +
		"────────────────────────────────────────────────────────────\n" +
		"feat: subject here\n\nbody line one\nbody line two\n" +
		"────────────────────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	d := parsePiDialog(screen)
	if d == nil || d.Kind != "editor" {
		t.Fatalf("parse: %+v", d)
	}
	if d.Scrolled || d.HiddenAbove != 0 || d.HiddenBelow != 0 {
		t.Fatalf("a short buffer is not a fragment: %+v", d)
	}
	if d.Title != "Commit staged changes with this message?" {
		t.Fatalf("title=%q", d.Title)
	}
	if want := "feat: subject here\n\nbody line one\nbody line two"; d.Body != want {
		t.Fatalf("body=%q, want %q", d.Body, want)
	}
}
