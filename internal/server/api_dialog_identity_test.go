package server

import (
	"net/http"
	"testing"
	"time"
)

// A yes/no (and escape) answer must land on the dialog it was asked
// for: the dispatch re-read verifies the pane still shows that exact
// dialog (bridge title, or the synthesized pane id), not just any
// confirm hint. pi moving on to a different confirm must not get
// Enter'd blind.
func TestDialogAnswerTargetsTheShownDialog(t *testing.T) {
	// The pane shows the push confirm; the card was minted for the
	// edit confirm.
	pane := confirmPaint("Allow push?")
	a, sessPath := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath, pane, pane, pane, pane)
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(t.Context(), sessPath)
		return err == nil
	})

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "old", Kind: "confirm", Title: "Allow edit?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	if rec := postAnswer(t, a, `{"id":"old","answer":"yes"}`); rec.Code != http.StatusConflict {
		t.Fatalf("yes on a replaced confirm: code=%d, want 409", rec.Code)
	}
	if n := keyCounts(f)["enter"]; n != 0 {
		t.Fatalf("yes sent %d enters into the wrong dialog", n)
	}

	// Esc is dismiss-safe: it goes out even when the shown dialog is
	// not the one the card minted, so a cancel can never strand.
	if rec := postAnswer(t, a, `{"id":"old","answer":"escape"}`); rec.Code != http.StatusOK {
		t.Fatalf("escape on a replaced confirm: code=%d, want 200", rec.Code)
	}
	if n := keyCounts(f)["esc"]; n != 1 {
		t.Fatalf("esc presses=%d, want the one esc", n)
	}
}
