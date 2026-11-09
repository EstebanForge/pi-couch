package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// hc Close is one graceful ctrl+d; the fake's pi exits on EOF, so no
// kill escalation follows.
func TestCloseSessionSendsCtrlD(t *testing.T) {
	a, sessPath := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath, confirmPaint("Allow?"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(t.Context(), sessPath)
		return err == nil
	})
	_ = sessPath

	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/close", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("close = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	graceful := false
	for _, keys := range f.keyLog() {
		if len(keys) == 1 && keys[0] == "ctrl+d" {
			graceful = true
		}
	}
	if !graceful {
		t.Fatalf("keys = %v, want a ctrl+d first", f.keyLog())
	}
	// The terminal answered the EOF: no kill escalation.
	if kills := f.killLog(); len(kills) != 0 {
		t.Fatalf("kills = %v, want none after a graceful exit", kills)
	}
}
