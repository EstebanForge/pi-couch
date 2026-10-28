package server

import (
	"testing"
	"time"
)

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

// A claim older than the TTL must be claimable again: pi re-presents
// identical dialog content (same pane id) minutes later, and the memo
// only guards the in-flight double-tap race.
func TestDialogMemosClaimExpiresAfterTTL(t *testing.T) {
	now := time.Unix(0, 0)
	m := newDialogMemos()
	m.now = func() time.Time { return now }
	if !m.Claim("d1") {
		t.Fatal("first claim must win")
	}
	if m.Claim("d1") {
		t.Fatal("claim inside the TTL must lose")
	}
	now = now.Add(dialogClaimTTL - time.Second)
	if m.Claim("d1") {
		t.Fatal("claim one tick inside the TTL must lose")
	}
	now = now.Add(2 * time.Second)
	if !m.Claim("d1") {
		t.Fatal("expired claim must be claimable again")
	}
	if m.Claim("d1") {
		t.Fatal("re-claimed id must lose again inside the fresh TTL")
	}
}

// Release must reach the prev generation: a claim that rotated can
// never otherwise be released, a second stuck-forever path.
func TestDialogMemosReleaseReachesPrev(t *testing.T) {
	m := newDialogMemos()
	m.cap = 1
	if !m.Claim("a") || !m.Claim("b") {
		t.Fatal("setup claims must win")
	}
	m.Release("a")
	if !m.Claim("a") {
		t.Fatal("release must drop rotated claims too")
	}
}
