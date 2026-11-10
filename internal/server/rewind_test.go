package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
)

// rewindFixture seeds a session with one user message (entry "a") and
// returns the app, its jsonl path, the mux, and the fake core. twoTurns
// appends an assistant reply and a second user message (entries "b"
// and "c").
func rewindFixture(t *testing.T, twoTurns bool) (*app, string, http.Handler, *hcFakeCore) {
	t.Helper()
	a, sessPath := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath, confirmPaint("Allow?"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(t.Context(), sessPath)
		return err == nil
	})
	// The rewind busy gate reads the driver badges; pi parked idle is
	// the rewindable state. The map holds the seed status, so the
	// change rides the same event the core would send. Wait until the
	// driver consumed it: a POST racing the event would read the
	// seed's working state and take the 409 busy branch forever.
	f.mu.Lock()
	f.state = "idle"
	f.mu.Unlock()
	f.event("status", "t1", map[string]any{
		"status":   map[string]any{"state": "idle", "app": "pi"},
		"reported": true,
	})
	waitForHC(t, func() bool {
		badges, _ := a.hcDrv.DriveStatus(t.Context())
		return badges[sessPath] == "idle"
	})
	if twoTurns {
		f2, err := os.OpenFile(sessPath, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(f2, "%s\n%s\n", asstLineServer("b", "a", 3), userLine("c", "b", "again"))
		f2.Close()
	}
	return a, sessPath, a.guard(a.routes()), f
}

func rewindPromptSent(f *hcFakeCore) string {
	sends := f.sendLog()
	if len(sends) == 0 {
		return ""
	}
	text, _ := sends[len(sends)-1]["text"].(string)
	return text
}

func rewindPost(t *testing.T, h http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+key+"/rewind", strings.NewReader(body))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func bridgeRewindPost(t *testing.T, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/bridge/rewind", strings.NewReader(body))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestRewindDrivesTheBridgeCommand(t *testing.T) {
	a, sessPath, h, f := rewindFixture(t, false)

	phone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		phone <- rewindPost(t, h, "s1", `{"seq":0}`)
	}()

	// The command must reach the core before a verdict can exist.
	waitForHC(t, func() bool { return rewindPromptSent(f) != "" })
	if rewindPromptSent(f) != "/couchpi-rewind a" {
		t.Fatalf("prompt: %q", rewindPromptSent(f))
	}
	res := bridgeRewindPost(t, h, fmt.Sprintf(`{"session_path":%q,"entry_id":"a","ok":true}`, sessPath))
	if res.Code != http.StatusNoContent {
		t.Fatalf("bridge verdict: %d", res.Code)
	}

	rec := <-phone
	if rec.Code != http.StatusOK {
		t.Fatalf("phone: %d %s", rec.Code, rec.Body.String())
	}
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out.OK {
		t.Fatalf("body: %s err %v", rec.Body.String(), err)
	}
	// The verdict-driven editor clear fires before the response lands:
	// one ctrl+c clears pi's desk refill.
	counts := keyCounts(f)
	if counts["ctrl+c"] != 1 {
		t.Fatalf("editor clear keys: %v", f.keyLog())
	}
	a.rewindMu.Lock()
	defer a.rewindMu.Unlock()
	if len(a.rewindPends) != 0 {
		t.Fatalf("pending left behind: %v", a.rewindPends)
	}
}

func TestRewindRefusesABusySession(t *testing.T) {
	a, sessPath, h, f := rewindFixture(t, false)
	f.mu.Lock()
	f.state = "working"
	f.mu.Unlock()
	f.event("status", "t1", map[string]any{
		"status":   map[string]any{"state": "working", "app": "pi"},
		"reported": true,
	})
	// Same event race as the fixture: wait until the driver consumed
	// working, or the POST could read idle and dispatch.
	waitForHC(t, func() bool {
		badges, _ := a.hcDrv.DriveStatus(t.Context())
		return badges[sessPath] == "working"
	})

	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code: %d %s", rec.Code, rec.Body.String())
	}
	if rewindPromptSent(f) != "" {
		t.Fatalf("prompt dispatched: %q", rewindPromptSent(f))
	}
	if len(f.keyLog()) != 0 {
		t.Fatalf("clear keys dispatched: %v", f.keyLog())
	}
}

func TestRewindTargetsOnlyTheLastUserMessage(t *testing.T) {
	_, _, h, f := rewindFixture(t, true)

	// seq 0 is the first user message; "c" at seq 2 is the last one.
	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("not_last code: %d %s", rec.Code, rec.Body.String())
	}
	if rewindPromptSent(f) != "" {
		t.Fatalf("prompt dispatched: %q", rewindPromptSent(f))
	}
}

func TestRewindRejectsNonUserAndMissingTargets(t *testing.T) {
	_, _, h, _ := rewindFixture(t, true)

	// seq 99 is off the branch; seq 1 is the assistant's text item.
	rec := rewindPost(t, h, "s1", `{"seq":99}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("missing seq: %d %s", rec.Code, rec.Body.String())
	}
	rec = rewindPost(t, h, "s1", `{"seq":1}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("assistant target: %d %s", rec.Code, rec.Body.String())
	}
}

func TestRewindTimesOutWithoutABridgeVerdict(t *testing.T) {
	a, _, h, _ := rewindFixture(t, false) // a: the pending cleanup check below

	old := rewindVerdictWait
	rewindVerdictWait = 60 * time.Millisecond
	defer func() { rewindVerdictWait = old }()

	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code: %d", rec.Code)
	}
	var out struct {
		OK   bool   `json:"ok"`
		Code string `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.OK || out.Code != "timeout" {
		t.Fatalf("body: %s", rec.Body.String())
	}
	a.rewindMu.Lock()
	defer a.rewindMu.Unlock()
	if len(a.rewindPends) != 0 {
		t.Fatalf("pending left behind: %v", a.rewindPends)
	}
}

func TestRewindDoubleSubmitIsBusy(t *testing.T) {
	a, _, h, f := rewindFixture(t, false)
	a.rewindMu.Lock()
	a.rewindPends = map[string]*rewindPending{"s1": {entryID: "a", verdict: make(chan rewindResult, 1)}}
	a.rewindMu.Unlock()

	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code: %d %s", rec.Code, rec.Body.String())
	}
	if rewindPromptSent(f) != "" {
		t.Fatalf("prompt dispatched: %q", rewindPromptSent(f))
	}
}

func TestRewindDispatchFailureLeavesNoPending(t *testing.T) {
	a, _, h, _ := rewindFixture(t, false)
	// A driver that can never sync makes TargetForPath fail (not
	// driven), so the dispatch dies before any wait.
	a.hcDrv = hc.NewDriver(hc.NewAt(filepath.Join(t.TempDir(), "dead.sock")))

	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("not driven: %d %s", rec.Code, rec.Body.String())
	}
	a.rewindMu.Lock()
	defer a.rewindMu.Unlock()
	if len(a.rewindPends) != 0 {
		t.Fatalf("pending left behind: %v", a.rewindPends)
	}
}

func TestBridgeRewindVerdictMatching(t *testing.T) {
	a, sessPath, h, _ := rewindFixture(t, false)
	old := rewindVerdictWait
	rewindVerdictWait = 3 * time.Second
	defer func() { rewindVerdictWait = old }()

	phone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		phone <- rewindPost(t, h, "s1", `{"seq":0}`)
	}()
	waitForHC(t, func() bool {
		a.rewindMu.Lock()
		defer a.rewindMu.Unlock()
		return len(a.rewindPends) == 1
	})

	// A verdict for a different entry must not resolve the wait.
	bridgeRewindPost(t, h, fmt.Sprintf(`{"session_path":%q,"entry_id":"zzz","ok":true}`, sessPath))
	a.rewindMu.Lock()
	if len(a.rewindPends) != 1 {
		a.rewindMu.Unlock()
		t.Fatal("mismatched verdict resolved the pending")
	}
	a.rewindMu.Unlock()

	// A verdict for another session must not resolve it either.
	bridgeRewindPost(t, h, `{"session_path":"/other/s.jsonl","entry_id":"a","ok":true}`)
	a.rewindMu.Lock()
	if len(a.rewindPends) != 1 {
		a.rewindMu.Unlock()
		t.Fatal("foreign session verdict resolved the pending")
	}
	a.rewindMu.Unlock()

	bridgeRewindPost(t, h, fmt.Sprintf(`{"session_path":%q,"entry_id":"a","ok":true}`, sessPath))
	rec := <-phone
	var out struct {
		OK bool `json:"ok"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil || !out.OK {
		t.Fatalf("body: %s err %v", rec.Body.String(), err)
	}
}

func TestRewindUnknownSession(t *testing.T) {
	_, _, h, _ := rewindFixture(t, false)
	rec := rewindPost(t, h, "nope", `{"seq":0}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code: %d", rec.Code)
	}
}
