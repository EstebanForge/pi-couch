package server

import (
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"
)

// Close is two ctrl+ds: the first exits pi, the second ends the shell
// so the pane dies with it — sibling agents in the workspace survive.
func TestCloseSessionSendsTwoCtrlD(t *testing.T) {
	var sends, paneCloses, wsCloses int32
	var keysLog [][]string
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath:   &sessPath,
		sends:      &sends,
		keysLog:    &keysLog,
		paneCloses: &paneCloses,
		wsCloses:   &wsCloses,
	})
	a, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	_ = a

	req := httptest.NewRequest("POST", "/api/sessions/s1/close", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)

	if rec.Code != 200 {
		t.Fatalf("close = %d, want 200 (%s)", rec.Code, rec.Body.String())
	}
	if len(keysLog) != 2 {
		t.Fatalf("send_input calls = %d, want 2 (%v)", len(keysLog), keysLog)
	}
	for i, keys := range keysLog {
		if len(keys) != 1 || keys[0] != "ctrl+d" {
			t.Fatalf("keys[%d] = %v, want [ctrl+d]", i, keys)
		}
	}
	if n := atomic.LoadInt32(&paneCloses); n != 1 {
		t.Fatalf("pane.close calls = %d, want 1", n)
	}
	if n := atomic.LoadInt32(&wsCloses); n != 0 {
		t.Fatalf("workspace.close calls = %d, want 0 (siblings survive)", n)
	}
}
