package server

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
)

// rewindFixture seeds a session with one user message (entry "a") and
// returns the app, its jsonl path, the mux, and the fake's prompt
// recorder. twoTurns appends an assistant reply and a second user
// message (entries "b" and "c").
func rewindFixture(t *testing.T, twoTurns bool) (*app, string, http.Handler, *atomic.Value, *[][]string) {
	t.Helper()
	var sends int32
	var sessPath string
	var lastPrompt atomic.Value
	var status atomic.Value
	status.Store("idle")
	pane := "ready"
	keys := [][]string{}
	sockPath := herdrFake(t, fakeHerdrOpts{
		sessPath:       &sessPath,
		sends:          &sends,
		status:         &status,
		paneText:       &pane,
		lastPromptText: &lastPrompt,
		keysLog:        &keys,
	})
	a, sub, h := streamTestAppWithHerdr(t, sockPath)
	sessPath = filepath.Join(sub, "s1.jsonl")
	if twoTurns {
		f, err := os.OpenFile(sessPath, os.O_APPEND|os.O_WRONLY, 0o644)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(f, "%s\n%s\n", asstLineServer("b", "a", 3), userLine("c", "b", "again"))
		f.Close()
	}
	return a, sessPath, h, &lastPrompt, &keys
}

func promptSent(lastPrompt *atomic.Value) string {
	v, _ := lastPrompt.Load().(string)
	return v
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
	a, sessPath, h, lastPrompt, keys := rewindFixture(t, false)

	phone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		phone <- rewindPost(t, h, "s1", `{"seq":0}`)
	}()

	// The command must reach herdr before a verdict can exist.
	waitForServer(t, func() bool { return promptSent(lastPrompt) != "" })
	if promptSent(lastPrompt) != "/couchpi-rewind a" {
		t.Fatalf("prompt: %q", promptSent(lastPrompt))
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
	// The verdict-driven editor clear fires before the response lands.
	if len(*keys) != 1 || (*keys)[0][0] != "ctrl+c" {
		t.Fatalf("editor clear keys: %v", *keys)
	}
	a.rewindMu.Lock()
	defer a.rewindMu.Unlock()
	if len(a.rewindPends) != 0 {
		t.Fatalf("pending left behind: %v", a.rewindPends)
	}
}

func TestRewindRefusesABusySession(t *testing.T) {
	a, sessPath, h, lastPrompt, keys := rewindFixture(t, false)
	a.status = &fakeStatus{badges: map[string]string{sessPath: "working"}}

	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code: %d %s", rec.Code, rec.Body.String())
	}
	if promptSent(lastPrompt) != "" {
		t.Fatalf("prompt dispatched: %q", promptSent(lastPrompt))
	}
	if len(*keys) != 0 {
		t.Fatalf("clear keys dispatched: %v", *keys)
	}
}

func TestRewindTargetsOnlyTheLastUserMessage(t *testing.T) {
	_, _, h, lastPrompt, _ := rewindFixture(t, true)

	// seq 0 is the first user message; "c" at seq 2 is the last one.
	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("not_last code: %d %s", rec.Code, rec.Body.String())
	}
	if promptSent(lastPrompt) != "" {
		t.Fatalf("prompt dispatched: %q", promptSent(lastPrompt))
	}
}

func TestRewindRejectsNonUserAndMissingTargets(t *testing.T) {
	_, _, h, _, _ := rewindFixture(t, true)

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
	a, _, h, _, _ := rewindFixture(t, false)
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
	a, _, h, lastPrompt, _ := rewindFixture(t, false)
	a.rewindMu.Lock()
	a.rewindPends = map[string]*rewindPending{"s1": {entryID: "a", verdict: make(chan rewindResult, 1)}}
	a.rewindMu.Unlock()

	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code: %d %s", rec.Code, rec.Body.String())
	}
	if promptSent(lastPrompt) != "" {
		t.Fatalf("prompt dispatched: %q", promptSent(lastPrompt))
	}
}

func TestRewindDispatchFailureLeavesNoPending(t *testing.T) {
	a, _, h, _, _ := rewindFixture(t, false)
	// A status source with no targets makes TargetForPath fail
	// (not driven), so the dispatch dies before any wait.
	a.status = &fakeStatus{badges: map[string]string{}}

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
	a, sessPath, h, _, _ := rewindFixture(t, false)
	old := rewindVerdictWait
	rewindVerdictWait = 3 * time.Second
	defer func() { rewindVerdictWait = old }()

	phone := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		phone <- rewindPost(t, h, "s1", `{"seq":0}`)
	}()
	waitForServer(t, func() bool {
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

func TestRewindRefusesHCWithoutAClearVerb(t *testing.T) {
	a, _, h, lastPrompt, keys := rewindFixture(t, false)
	// hc carries the dialog primitives but no keys verb: the desk
	// refill would sit uncleared in the pane editor.
	a.hcDrv = hc.NewDriver(hc.NewAt(filepath.Join(t.TempDir(), "hc.sock")))

	rec := rewindPost(t, h, "s1", `{"seq":0}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code: %d %s", rec.Code, rec.Body.String())
	}
	if promptSent(lastPrompt) != "" {
		t.Fatalf("prompt dispatched: %q", promptSent(lastPrompt))
	}
	if len(*keys) != 0 {
		t.Fatalf("clear keys dispatched: %v", *keys)
	}
}

func TestRewindUnknownSession(t *testing.T) {
	_, _, h, _, _ := rewindFixture(t, false)
	rec := rewindPost(t, h, "nope", `{"seq":0}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("code: %d", rec.Code)
	}
}
