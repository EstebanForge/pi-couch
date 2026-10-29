package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// A yes/no (and escape) answer must land on the dialog it was asked
// for: the dispatch re-read verifies the pane still shows that exact
// dialog (bridge title, or the synthesized pane id), not just any
// confirm hint. pi moving on to a different confirm must not get
// Enter'd blind.
func TestDialogAnswerTargetsTheShownDialog(t *testing.T) {
	var sends int32
	var sessPath string
	pane := confirmPaint("Allow push?") // pi moved on to a different confirm
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneText: &pane, paneAfterSend: &pane})
	a, sub, _ := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{ID: "old", Kind: "confirm", Title: "Allow edit?"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	post := func(body string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.guard(a.routes()).ServeHTTP(rec, req)
		return rec.Code
	}

	if code := post(`{"id":"old","answer":"yes"}`); code != http.StatusConflict {
		t.Fatalf("yes on a replaced confirm: code=%d, want 409", code)
	}
	if n := atomic.LoadInt32(&sends); n != 0 {
		t.Fatalf("yes sent %d key batches into the wrong dialog", n)
	}

	if code := post(`{"id":"old","answer":"escape"}`); code != http.StatusConflict {
		t.Fatalf("escape on a replaced confirm: code=%d, want 409", code)
	}
	if n := atomic.LoadInt32(&sends); n != 0 {
		t.Fatalf("escape sent %d key batches into the wrong dialog", n)
	}

	// The shown dialog answers: same id, title back on screen.
	pane = confirmPaint("Allow edit?")
	if code := post(`{"id":"old","answer":"yes"}`); code != http.StatusOK {
		t.Fatalf("yes on the shown confirm: code=%d", code)
	}
	if n := atomic.LoadInt32(&sends); n != 1 {
		t.Fatalf("sends=%d, want 1", n)
	}
}
