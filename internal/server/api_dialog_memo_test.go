package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// confirmPaint builds pi's confirm dialog paint for the given title,
// cursor on Yes.
func confirmPaint(title string) string {
	return title + "\n\n→ Yes\n  No\n\n↑↓ navigate  enter select  escape/ctrl+c cancel\n"
}

func bridgeReal(t *testing.T, a *app, sessPath string) {
	t.Helper()
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{Kind: "confirm", Title: "Allow?", ID: "real"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()
}

// memoAnswer posts one yes answer through the guarded routes.
func memoAnswer(t *testing.T, a *app) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer",
		strings.NewReader(`{"id":"real","answer":"yes"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	return rec
}

// The double-tap: the second answer for the same dialog id must be
// refused without a second Enter, because the first Enter may already
// sit in the pane and would answer whatever pi shows next.
func TestDialogAnswerMemoDedupesDoubleTap(t *testing.T) {
	a, sessPath := hcDialogApp(t)
	confirm := confirmPaint("Allow?")
	drv, f := startHCFake(t, sessPath, confirm, confirm, confirm)
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(t.Context(), sessPath)
		return err == nil
	})
	bridgeReal(t, a, sessPath)

	if rec := memoAnswer(t, a); rec.Code != http.StatusOK {
		t.Fatalf("first answer: code=%d, want 200", rec.Code)
	}
	if n := keyCounts(f)["enter"]; n != 1 {
		t.Fatalf("enters=%d, want 1", n)
	}
	if rec := memoAnswer(t, a); rec.Code != http.StatusConflict {
		t.Fatalf("second answer: code=%d, want 409", rec.Code)
	}
	if n := keyCounts(f)["enter"]; n != 1 {
		t.Fatalf("enters after dup=%d, want still 1", n)
	}
}

// A send that provably failed releases the claim: the user may retry
// once the core is healthy again and must not hit the memo wall.
func TestDialogAnswerMemoReleasesOnSendFailure(t *testing.T) {
	a, sessPath := hcDialogApp(t)
	confirm := confirmPaint("Allow?")
	drv, f := startHCFake(t, sessPath, confirm, confirm, confirm, confirm)
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(t.Context(), sessPath)
		return err == nil
	})
	bridgeReal(t, a, sessPath)
	f.failKeysNext.Store(1) // the dispatch read succeeds; the enter press fails once

	if rec := memoAnswer(t, a); rec.Code != http.StatusBadGateway {
		t.Fatalf("failing send: code=%d, want 502", rec.Code)
	}
	if rec := memoAnswer(t, a); rec.Code != http.StatusOK {
		t.Fatalf("retry after failure: code=%d, want 200", rec.Code)
	}
	if n := keyCounts(f)["enter"]; n != 1 {
		t.Fatalf("successful enters=%d, want 1 (the failed press left no keys)", n)
	}
}

// pi re-presents identical dialog content later (same pane id, the
// 2026-10-28 stuck-commit incident): the claim must expire so the new
// instance answers instead of 409ing forever.
func TestDialogAnswerMemoAllowsRePresentedDialog(t *testing.T) {
	a, sessPath := hcDialogApp(t)
	confirm := confirmPaint("Allow?")
	drv, f := startHCFake(t, sessPath, confirm, confirm, confirm, confirm, confirm)
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(t.Context(), sessPath)
		return err == nil
	})
	bridgeReal(t, a, sessPath)

	if rec := memoAnswer(t, a); rec.Code != http.StatusOK {
		t.Fatalf("first answer: code=%d, want 200", rec.Code)
	}
	if rec := memoAnswer(t, a); rec.Code != http.StatusConflict {
		t.Fatalf("double tap: code=%d, want 409", rec.Code)
	}
	a.answered.now = func() time.Time { return time.Now().Add(2 * dialogClaimTTL) }
	if rec := memoAnswer(t, a); rec.Code != http.StatusOK {
		t.Fatalf("re-presented dialog after TTL: code=%d, want 200", rec.Code)
	}
	if n := keyCounts(f)["enter"]; n != 2 {
		t.Fatalf("enters=%d, want 2", n)
	}
}
