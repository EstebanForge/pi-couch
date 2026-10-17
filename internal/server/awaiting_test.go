package server

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// The chrome lines come from a live pi /model picker (2026-10-13 probe)
// plus herdr-web-ui's measured pi 1.2 signatures (prompt.ts PI_*_RE):
// the select/confirm menu and the input line paint space-separated
// hints with no middle dot, so the generic hint-bar regex never saw them.
//
//	Enter to select · Ctrl+S to set as default · Escape/Ctrl+C to cancel
func TestDialogOpenSignatures(t *testing.T) {
	modelPicker := "\x1b[2m  Enter to select · Ctrl+S to set as default · Escape/Ctrl+C to cancel\x1b[0m"
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"model picker hint bar", modelPicker, true},
		{"extension editor external hint", "some draft\n  ctrl+g external editor\n", true},
		{"select counter", "  (3/25)", true},
		{"pi select menu hint", "\x1b[2m↑↓ navigate  enter select  escape/ctrl+c cancel\x1b[0m", true},
		{"pi input hint", "  enter submit  escape/ctrl+c cancel", true},
		{"pi hint with footer below", "↑↓ navigate  enter select  escape/ctrl+c cancel\n~/Dev/EstebanForge/couch-pi\n55.5k/1.0M tokens", true},
		{"buried answered hint", "↑↓ navigate  enter select  escape/ctrl+c cancel\n" + strings.Repeat("some later transcript line\n", 8), false},
		{"prose mentions enter and cancel", "you can press enter to select an option or escape to cancel at any time", false},
		{"plain transcript", "The user asks to reply with just ok.\n", false},
		{"token footer", "↑55k ↓15 $0.008 5.5%/1.0M (auto)   (zai) glm-5.3-flash · high\n", false},
		{"empty", "", false},
	}
	for _, tc := range cases {
		if got := dialogOpen(tc.text); got != tc.want {
			t.Errorf("%s: dialogOpen=%v, want %v", tc.name, got, tc.want)
		}
	}
}

type fakePaneHerdr struct {
	agents []herdr.Agent
	panes  map[string]string // pane id -> screen text
}

func (f *fakePaneHerdr) Agents(ctx context.Context) ([]herdr.Agent, error) { return f.agents, nil }

func (f *fakePaneHerdr) PaneRead(ctx context.Context, paneID, source string, lines int) (herdr.PaneRead, error) {
	return herdr.PaneRead{Text: f.panes[paneID]}, nil
}

func TestAwaitingDetectorReadsEveryAgent(t *testing.T) {
	f := &fakePaneHerdr{
		agents: []herdr.Agent{
			{Name: "a", AgentStatus: "working", PaneID: "p1", Session: &herdr.AgentSessionRef{Value: "/s/a.jsonl"}},
			{Name: "b", AgentStatus: "done", PaneID: "p2", Session: &herdr.AgentSessionRef{Value: "/s/b.jsonl"}},
		},
		panes: map[string]string{
			// The live lesson: pi's model picker parks the pane while
			// herdr reports done - a done agent with chrome IS waiting.
			"p1": "Enter to select · Ctrl+S to set as default",
			"p2": "Enter to select · Ctrl+S to set as default",
		},
	}
	d := newAwaitingDetector(f, time.Hour)
	d.tick(context.Background())

	if !d.isAwaiting("/s/a.jsonl") || !d.isAwaiting("/s/b.jsonl") {
		t.Error("dialog chrome must flag awaiting regardless of herdr status")
	}

	// Dialog closes: chrome gone from the pane.
	f.panes["p1"] = "plain output"
	d.tick(context.Background())
	if d.isAwaiting("/s/a.jsonl") {
		t.Error("chrome gone: awaiting should clear")
	}
	if !d.isAwaiting("/s/b.jsonl") {
		t.Error("b still carries chrome")
	}

	// Agent vanishes: path pruned.
	f.agents = nil
	d.tick(context.Background())
	if d.isAwaiting("/s/a.jsonl") || d.isAwaiting("/s/b.jsonl") {
		t.Error("vanished agents: paths should prune")
	}
}

func TestAwaitingDetectorNilSafe(t *testing.T) {
	var d *awaitingDetector
	if d.isAwaiting("/s/a.jsonl") {
		t.Error("nil detector must read as not awaiting")
	}
	if d.snapshot() == nil {
		t.Error("nil detector snapshot must be non-nil for JSON")
	}
}

func TestParseMenuScreens(t *testing.T) {
	selectScreen := "Some agent text\n" +
		"\n" +
		"\x1b[36m→ pick me\x1b[0m\n" +
		"  second choice\n" +
		"  third\n" +
		"\n" +
		"↑↓ navigate  enter select  escape/ctrl+c cancel\n" +
		"\n" +
		"/tmp/repo (main)\n" +
		"92k context"
	m := parsePiDialog(selectScreen)
	if m == nil {
		t.Fatal("select screen parsed nil")
	}
	if len(m.Options) != 3 || m.Options[0].Label != "pick me" || m.Options[2].Label != "third" {
		t.Fatalf("options=%v", m.Options)
	}
	if m.Cursor != 0 {
		t.Fatalf("cursor=%d, want 0", m.Cursor)
	}

	mid := "title\n\n→ alpha\n  beta\n  gamma\n\n↑↓ navigate  enter select  escape/ctrl+c cancel\n"
	if m := parsePiDialog(mid); m == nil || m.Cursor != 0 || m.Options[1].Label != "beta" || m.Title != "title" {
		t.Fatalf("mid=%v", m)
	}
	if aimSteps(0, 2, 3) != 2 || aimSteps(2, 1, 3) != 2 || aimSteps(1, 1, 3) != 0 {
		t.Fatal("aimSteps wrong")
	}

	wrapped := "→ the first option that is quite long and\ncontinues here\n  short\n\n↑↓ navigate  enter select  escape/ctrl+c cancel\n"
	if m := parsePiDialog(wrapped); m == nil || m.Options[0].Label != "the first option that is quite long and continues here" {
		t.Fatalf("wrapped=%v", m)
	}

	if parsePiDialog("just prose, no dialog here\n") != nil {
		t.Fatal("prose parsed as menu")
	}
	if parsePiDialog("→ orphan rows without any hint\n") != nil {
		t.Fatal("hint-less rows parsed as menu")
	}
}
