package server

import (
	"context"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// The chrome lines come from a live pi /model picker (2026-10-13 probe):
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
