package server

import (
	"testing"

	"github.com/EstebanForge/couch-pi/internal/pi"
)

func vitems(specs ...string) []pi.ViewItem {
	out := make([]pi.ViewItem, len(specs))
	for i, s := range specs {
		out[i] = pi.ViewItem{Kind: "text", Text: s}
	}
	return out
}

func TestChatAppendPureAppend(t *testing.T) {
	prev := itemFps(vitems("a", "b"))
	items := vitems("a", "b", "c", "d")
	cur := itemFps(items)
	delta, ok := chatAppend(prev, items, cur, 0)
	if !ok {
		t.Fatal("pure append flagged as full render")
	}
	if len(delta) != 2 || delta[0].Text != "c" || delta[1].Text != "d" {
		t.Fatalf("delta = %v", delta)
	}
}

func TestChatAppendNoChange(t *testing.T) {
	prev := itemFps(vitems("a", "b"))
	items := vitems("a", "b")
	delta, ok := chatAppend(prev, items, itemFps(items), 0)
	if !ok {
		t.Fatal("unchanged list flagged as full render")
	}
	if len(delta) != 0 {
		t.Fatalf("delta = %v, want empty", delta)
	}
}

// A full tail window slides: growth drops the oldest item, and Seq
// gives the exact slide. Strict prefix compares can never append once
// the window is full — the post-resume freeze bug.
func TestChatAppendSlidingWindow(t *testing.T) {
	prev := itemFps(vitems("a", "b", "c"))
	items := vitems("b", "c", "d")
	for i := range items {
		items[i].Seq = i + 1
	}
	delta, ok := chatAppend(prev, items, itemFps(items), 0)
	if !ok {
		t.Fatal("sliding-window growth flagged as full render")
	}
	if len(delta) != 1 || delta[0].Text != "d" {
		t.Fatalf("delta = %v, want [d]", delta)
	}
}

func TestChatAppendRewriteIsFullRender(t *testing.T) {
	prev := itemFps(vitems("a", "b", "c"))
	items := vitems("x", "y", "z")
	if _, ok := chatAppend(prev, items, itemFps(items), 0); ok {
		t.Fatal("a rewritten window passed as an append")
	}
}

func TestChatAppendEditedItemForcesFull(t *testing.T) {
	prev := itemFps(vitems("a", "b"))
	items := vitems("a", "EDITED")
	if _, ok := chatAppend(prev, items, itemFps(items), 0); ok {
		t.Fatal("edited item not flagged as full render")
	}
}

func TestChatAppendShrinkForcesFull(t *testing.T) {
	prev := itemFps(vitems("a", "b", "c"))
	items := vitems("a")
	if _, ok := chatAppend(prev, items, itemFps(items), 0); ok {
		t.Fatal("shrunk list flagged as append")
	}
}

// Polling agents repeat one tool pair over and over, so a FULL window
// can be content-ambiguous: a two-item slide looks byte-identical to
// no change when only fingerprints are compared, and the new items
// would be lost as an empty delta. Seq pins the true slide.
func TestChatAppendPeriodicNoAlias(t *testing.T) {
	base := make([]pi.ViewItem, 40)
	for i := range base {
		if i%2 == 0 {
			base[i] = pi.ViewItem{Kind: "text", Text: "x"}
		} else {
			base[i] = pi.ViewItem{Kind: "text", Text: "y"}
		}
	}
	prev := itemFps(base)
	items := make([]pi.ViewItem, 40)
	copy(items, base)
	for i := range items {
		items[i].Seq = 2 + i
	}
	cur := itemFps(items)
	delta, ok := chatAppend(prev, items, cur, 0)
	if !ok {
		t.Fatal("periodic slide flagged as full render")
	}
	if len(delta) != 2 || delta[0].Text != "x" || delta[1].Text != "y" {
		t.Fatalf("delta = %d items, want the 2 new ones", len(delta))
	}
}

func TestChatAppendSeqJumpForcesFull(t *testing.T) {
	prev := itemFps(vitems("a", "b", "c"))
	items := vitems("d", "e", "f")
	for i := range items {
		items[i].Seq = 10 + i
	}
	if _, ok := chatAppend(prev, items, itemFps(items), 0); ok {
		t.Fatal("seq jump past the window passed as an append")
	}
}

func TestItemFpsDistinguishContent(t *testing.T) {
	a := itemFps(vitems("hello"))
	b := itemFps(vitems("hellp"))
	if a[0] == b[0] {
		t.Fatal("different texts share a fingerprint")
	}
	if a[0] != itemFps(vitems("hello"))[0] {
		t.Fatal("same text, different fingerprint")
	}
	x := itemFps([]pi.ViewItem{{Kind: "text", Text: "x"}})
	y := itemFps([]pi.ViewItem{{Kind: "tool_call", Text: "x"}})
	if x[0] == y[0] {
		t.Fatal("different kinds share a fingerprint")
	}
}
