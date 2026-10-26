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
	followUpPanel := "┌ follow-ups (2) · after this run ─────────────\n" +
		"│ ○ Do Desktop e2e flow coverage.\n│\n" +
		"│ ○ Then remove the blocked if the new flow solves it.\n" +
		"│ alt+enter add follow-up · enter send next · alt+shift+up follow-up edit\n│\n" +
		"└────────────────────────────\n" +
		"── ⚙ Working ────────────────────\n"
	cases := []struct {
		name string
		text string
		want bool
	}{
		{"model picker hint bar", modelPicker, true},
		{"follow-up queue panel while working", followUpPanel, false},
		{"hint bar quoted mid-transcript", "earlier reply text\nenter to select · ctrl+s default · escape to cancel\n" + strings.Repeat("more transcript below\n", 6) + "~/Dev/x (main)\n↑55k ↓15 $0.008 (zai) glm-5.3-flash · high\n", false},
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

// The follow-ups queue panel paints its own middle-dot hint bar; the
// detector must treat it as a working session with queued messages,
// not as a parked dialog (field report 2026-10-25: the chat showed
// "Waiting for input" while pi held three queued follow-ups).
// The verbatim follow-ups queue paint from the field screenshot:
// panel rule, ○ rows, the panel's own hint bar, then pi's footer.
const followUpQueuePaint = "╭─ follow-ups (3) · after this run ──╮\n" +
	"○ The about screen still is displaying the old logo: /tmp/herdr-clipboard-ima…\n" +
	"○ In readme, on top of <h1 align=\"center\">QuotaGauge</h1>, add our new logo,\n" +
	"○ The header window bar here /tmp/herdr-clipboard-images-1000/client-1715-cl…\n" +
	"alt+enter add follow-up · enter send next · alt+up edit\n" +
	"── : Working ──\n" +
	"|\n" +
	"~/Dev/EstebanForge/QuotaGauge (main) · QuotaGauge\n" +
	"↑8.1M ↓382k R128.7M CH98.9%  $0.1407 20.9%/1.0M\n" +
	"💻 ATTD-Zenless 🧠|\n"

func TestParseFollowUpQueue(t *testing.T) {
	rows := parseFollowUpQueue(followUpQueuePaint)
	if len(rows) != 3 {
		t.Fatalf("rows = %q, want 3", rows)
	}
	if rows[0] != "The about screen still is displaying the old logo: /tmp/herdr-clipboard-ima…" {
		t.Fatalf("rows[0] = %q", rows[0])
	}
	if rows[2] != "The header window bar here /tmp/herdr-clipboard-images-1000/client-1715-cl…" {
		t.Fatalf("rows[2] = %q", rows[2])
	}
	if parseFollowUpQueue("plain transcript output\nmore output\n") != nil {
		t.Error("no panel must parse as no queue")
	}
}

func TestAwaitingDetectorCarriesQueue(t *testing.T) {
	f := &fakePaneHerdr{
		agents: []herdr.Agent{
			{Name: "a", AgentStatus: "working", PaneID: "p1", Session: &herdr.AgentSessionRef{Value: "/s/q.jsonl"}},
		},
		panes: map[string]string{"p1": followUpQueuePaint},
	}
	d := newAwaitingDetector(f, time.Hour)
	d.tick(context.Background())

	if rows := d.queued("/s/q.jsonl"); len(rows) != 3 {
		t.Fatalf("queued = %q, want 3", rows)
	}
	if d.isAwaiting("/s/q.jsonl") {
		t.Error("a queue is a working session, not a parked dialog")
	}

	// The queue drains: the panel leaves the paint.
	f.panes["p1"] = "plain output"
	d.tick(context.Background())
	if rows := d.queued("/s/q.jsonl"); len(rows) != 0 {
		t.Fatalf("queued after drain = %q, want none", rows)
	}
}

func TestAwaitingDetectorIgnoresFollowUpQueue(t *testing.T) {
	f := &fakePaneHerdr{
		agents: []herdr.Agent{
			{Name: "a", AgentStatus: "working", PaneID: "p1", Session: &herdr.AgentSessionRef{Value: "/s/q.jsonl"}},
		},
		panes: map[string]string{
			"p1": "╭─ follow-ups (3) · after this run ──╮\n" +
				"○ The about screen still is displaying the old logo: /tmp/herdr-clipboard-ima…\n" +
				"○ In readme, on top of <h1 align=\"center\">QuotaGauge</h1>, add our new logo,\n" +
				"○ The header window bar here /tmp/herdr-clipboard-images-1000/client-1715-cl…\n" +
				"alt+enter add follow-up · enter send next · alt+up edit\n" +
				"── : Working ──\n" +
				"|\n" +
				"~/Dev/EstebanForge/QuotaGauge (main) · QuotaGauge\n" +
				"↑8.1M ↓382k R128.7M CH98.9%  $0.1407 20.9%/1.0M\n" +
				"💻 ATTD-Zenless 🧠|\n",
		},
	}
	d := newAwaitingDetector(f, time.Hour)
	d.tick(context.Background())

	if d.isAwaiting("/s/q.jsonl") {
		t.Error("a follow-ups queue is a working session, not a parked dialog")
	}
	if d.parsed("/s/q.jsonl") != nil {
		t.Error("the queue panel must not parse as an answerable dialog")
	}
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
