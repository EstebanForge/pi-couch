package server

import (
	"context"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// The empty-editor fallback must not ride a single paint: right after
// a submitted dialog pi repaints and can transiently read as an
// emptied editor, which flashed an empty card on the phone. A real
// stuck editor persists, so the synthetic needs two consecutive ticks.
func TestEmptyEditorSyntheticNeedsTwoTicks(t *testing.T) {
	empty := " Commit staged changes with this message?\n" +
		"\n" +
		"────────────────────────────────────────────\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor\n\n" +
		"────────────────────────────────────────────\n" +
		"~/.cache/probe (main)\n" +
		"↑181k ↓1.2k R181k CH98.9% $0.033 6.1%/1.0M (auto)\n" +
		"💻 example-box"

	hc := &fakePaneHerdr{
		agents: []herdr.Agent{{PaneID: "p1", Session: &herdr.AgentSessionRef{Value: "/s1.jsonl"}}},
		panes:  map[string]string{"p1": empty},
	}
	d := newAwaitingDetector(hc, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.run(ctx)
	d.tick(ctx)

	if p := d.parsed("/s1.jsonl"); p != nil {
		t.Fatalf("one empty paint surfaced %+v, want nil", p)
	}
	d.tick(ctx)
	p := d.parsed("/s1.jsonl")
	if p == nil || p.Kind != "editor" || p.Body != "" {
		t.Fatalf("two ticks: got %+v, want the empty editor", p)
	}

	// A non-editor paint resets the streak.
	hc.panes["p1"] = "plain transcript output\nno dialog here"
	d.tick(ctx)
	if p := d.parsed("/s1.jsonl"); p != nil {
		t.Fatalf("prose surfaced %+v, want nil", p)
	}
	hc.panes["p1"] = empty
	d.tick(ctx)
	if p := d.parsed("/s1.jsonl"); p != nil {
		t.Fatalf("one empty paint after prose surfaced %+v, want nil", p)
	}
	d.tick(ctx)
	if p := d.parsed("/s1.jsonl"); p == nil {
		t.Fatal("two ticks after prose: want the empty editor back")
	}
}
