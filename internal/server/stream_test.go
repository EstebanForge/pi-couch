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
	delta, ok := chatAppend(prev, items, cur)
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
	delta, ok := chatAppend(prev, items, itemFps(items))
	if !ok {
		t.Fatal("unchanged list flagged as full render")
	}
	if len(delta) != 0 {
		t.Fatalf("delta = %v, want empty", delta)
	}
}

// A full tail window slides: growth drops the oldest item, so prev
// aligns one-in from cur's head. Strict prefix compares can never
// append once the window is full — the post-resume freeze bug.
func TestChatAppendSlidingWindow(t *testing.T) {
	prev := itemFps(vitems("a", "b", "c"))
	items := vitems("b", "c", "d")
	delta, ok := chatAppend(prev, items, itemFps(items))
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
	if _, ok := chatAppend(prev, items, itemFps(items)); ok {
		t.Fatal("a rewritten window passed as an append")
	}
}

func TestChatAppendEditedItemForcesFull(t *testing.T) {
	prev := itemFps(vitems("a", "b"))
	items := vitems("a", "EDITED")
	if _, ok := chatAppend(prev, items, itemFps(items)); ok {
		t.Fatal("edited item not flagged as full render")
	}
}

func TestChatAppendShrinkForcesFull(t *testing.T) {
	prev := itemFps(vitems("a", "b", "c"))
	items := vitems("a")
	if _, ok := chatAppend(prev, items, itemFps(items)); ok {
		t.Fatal("shrunk list flagged as append")
	}
}

func TestChatAppendSlidingHeadAppends(t *testing.T) {
	// Window at the cap: the oldest item fell off, but the client
	// accumulates by seq and never drops, so the slide is still a delta.
	prev := itemFps(vitems("a", "b", "c"))
	items := vitems("b", "c", "d")
	delta, ok := chatAppend(prev, items, itemFps(items))
	if !ok {
		t.Fatal("slid window flagged as full render")
	}
	if len(delta) != 1 || delta[0].Text != "d" {
		t.Fatalf("delta = %v, want [d]", delta)
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
