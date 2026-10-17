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
