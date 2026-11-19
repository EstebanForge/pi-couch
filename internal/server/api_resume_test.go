package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/pi"
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

// resumeApp is hcDialogApp with one session whose header carries a
// chosen cwd: the resume flow reads the cwd from the session header.
func resumeApp(t *testing.T, cwd string) (*app, string) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s2.jsonl")
	header := fmt.Sprintf(`{"type":"session","version":3,"id":"11111111-2222-3333-4444-555555555555","timestamp":"2026-10-10T12:00:00.000Z","cwd":%q}`, cwd)
	writeLines(t, path, header, userLine("a", "", "first"))
	a := &app{
		logger:      slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:         pi.NewIndex(root),
		home:        testHome(),
		answered:    newDialogMemos(),
		models:      newModelsCache(filepath.Join(t.TempDir(), "models.json")),
		memory:      newMemoryProbe("", ""),
		bridge:      map[string]bridgeEntry{},
		dialogLatch: map[string]*dialogWire{},
	}
	return a, path
}

// A resume adopts the space owning the session's folder (ADR-0013):
// the desktop app gets a home for sessions the phone revived.
func TestSessionResumeAdoptsTheSessionFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}

	a, path := resumeApp(t, proj)
	drv, f := startHCFake(t, "/x/driving-something-else.jsonl")
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	spaceDoc(t, f,
		map[string]any{"id": "spQ", "name": "Quick", "folder": home, "tabs": []any{map[string]any{"id": "tbQ"}}},
		map[string]any{"id": "spB", "name": "Baluard", "folder": proj, "tabs": []any{map[string]any{"id": "tbB"}}},
	)

	type res struct {
		rec *httptest.ResponseRecorder
	}
	done := make(chan res, 1)
	go func() { done <- res{postResume(t, a, "s2", `{}`)} }()
	spawnReady(t, f, path)
	select {
	case r := <-done:
		if r.rec.Code != 200 {
			t.Fatalf("resume = %d, want 200: %s", r.rec.Code, r.rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume never answered")
	}
	runs := f.runLog()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	line, _ := runs[0]["line"].(string)
	if !strings.Contains(line, "--session") || !strings.Contains(line, path) {
		t.Fatalf("run line = %q, want --session %s", line, path)
	}
	labels, _ := runs[0]["labels"].(map[string]any)
	if labels["herdr.space"] != "spB" || labels["herdr.tab"] != "tbB" {
		t.Fatalf("labels = %v, want the owning space spB/tbB", labels)
	}
}

// The $HOME fallback (project folder gone) never infers: a catch-all
// space at home would swallow every orphaned session.
func TestSessionResumeFallbackHomeStaysUnlabeled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)

	a, path := resumeApp(t, "/tmp/couchpi-gone-project")
	drv, f := startHCFake(t, "/x/driving-something-else.jsonl")
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	spaceDoc(t, f, map[string]any{"id": "spQ", "name": "Quick", "folder": home, "tabs": []any{map[string]any{"id": "tbQ"}}})

	type res struct {
		rec *httptest.ResponseRecorder
	}
	done := make(chan res, 1)
	go func() { done <- res{postResume(t, a, "s2", `{}`)} }()
	spawnReady(t, f, path)
	select {
	case r := <-done:
		if r.rec.Code != 200 {
			t.Fatalf("resume = %d, want 200: %s", r.rec.Code, r.rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume never answered")
	}
	runs := f.runLog()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	cwd, _ := runs[0]["cwd"].(string)
	if cwd != home {
		t.Fatalf("run cwd = %q, want the home fallback %s", cwd, home)
	}
	labels, _ := runs[0]["labels"].(map[string]any)
	if len(labels) != 0 {
		t.Fatalf("labels = %v, want none", labels)
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
