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

func TestChatAppendSlidingHeadForcesFull(t *testing.T) {
	// Window at the cap: the oldest item fell off, so the client's list is
	// no longer a prefix - full render territory.
	prev := itemFps(vitems("a", "b", "c"))
	items := vitems("b", "c", "d")
	if _, ok := chatAppend(prev, items, itemFps(items)); ok {
		t.Fatal("slid window flagged as append")
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
