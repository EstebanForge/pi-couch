package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func postResume(t *testing.T, a *app, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+key+"/resume", strings.NewReader(body))
	req.SetPathValue("key", key)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiSessionResume(rec, req)
	return rec
}

// Resume spawns pi --session over the closed session's jsonl; the
// optional text rides as the first prompt.
func TestSessionResumeStartsAgentWithSessionFlag(t *testing.T) {
	a, path := hcDialogApp(t)
	// The fake's one terminal drives a foreign session, so the resume
	// target s1 is undriven and TargetForPath misses it.
	drv, f := startHCFake(t, "/x/driving-something-else.jsonl")
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	// The spawn readiness needs no snapshot: a foreign terminal never
	// carries s1, so the resume proceeds immediately.
	_ = path

	type res struct {
		rec *httptest.ResponseRecorder
	}
	done := make(chan res, 1)
	go func() { done <- res{postResume(t, a, "s1", `{"text":"hi again"}`)} }()
	spawnReady(t, f, path)

	var rec *httptest.ResponseRecorder
	select {
	case r := <-done:
		rec = r.rec
	case <-time.After(5 * time.Second):
		t.Fatal("resume never answered")
	}
	if rec.Code != 200 {
		t.Fatalf("resume = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	runs := f.runLog()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	line, _ := runs[0]["line"].(string)
	want := path
	if !strings.Contains(line, "--session") || !strings.Contains(line, want) {
		t.Fatalf("run line = %q, want --session %s", line, want)
	}
	sends := f.sendLog()
	if len(sends) != 1 || sends[0]["text"] != "hi again" {
		t.Fatalf("sends = %v, want one hi again prompt", sends)
	}
	var out struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Key != "s1" {
		t.Fatalf("key = %q, want s1", out.Key)
	}
}

// An empty resume body starts pi over the jsonl with no first prompt.
func TestSessionResumeIdleSkipsPrompt(t *testing.T) {
	a, path := hcDialogApp(t)
	drv, f := startHCFake(t, "/x/driving-something-else.jsonl")
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() { done <- postResume(t, a, "s1", `{}`) }()
	spawnReady(t, f, path)

	select {
	case rec := <-done:
		if rec.Code != 200 {
			t.Fatalf("resume = %d, want 200: %s", rec.Code, rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume never answered")
	}
	if n := len(f.sendLog()); n != 0 {
		t.Fatalf("sends = %d, want 0 (no text in the body)", n)
	}
	line, _ := f.runLog()[0]["line"].(string)
	if !strings.Contains(line, "--session") {
		t.Fatalf("run line = %q, want --session", line)
	}
}
