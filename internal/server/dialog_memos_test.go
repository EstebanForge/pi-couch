package server

import "testing"

func TestDialogMemosFirstWriterWins(t *testing.T) {
	m := newDialogMemos()
	if !m.Claim("d1") {
		t.Fatal("first claim must win")
	}
	if m.Claim("d1") {
		t.Fatal("second claim for the same id must lose")
	}
	m.Release("d1")
	if !m.Claim("d1") {
		t.Fatal("claim after release must win")
	}
	if m.Claim("d2") != true || m.Claim("d3") != true {
		t.Fatal("unrelated ids must not interfere")
	}
}

func TestDialogMemosRollOver(t *testing.T) {
	m := newDialogMemos()
	m.cap = 3
	for _, id := range []string{"a", "b", "c"} {
		if !m.Claim(id) {
			t.Fatalf("claim %s must win", id)
		}
	}
	// Overflow rotates: a moves to prev, d starts a fresh generation.
	if !m.Claim("d") {
		t.Fatal("claim after rollover must win")
	}
	if m.Claim("a") {
		t.Fatal("a survives in prev")
	}
	if m.Claim("c") {
		t.Fatal("c survives in prev")
	}
	// d is in the fresh generation, e forces another rollover that
	// drops a and b but keeps d.
	if !m.Claim("e") || !m.Claim("f") {
		t.Fatal("claims must keep winning")
	}
	if m.Claim("d") {
		t.Fatal("d must survive one rollover")
	}
}
