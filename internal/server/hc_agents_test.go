package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func hcPost(t *testing.T, a *app, target, key string, body map[string]any) *httptest.ResponseRecorder {
	t.Helper()
	b, err := json.Marshal(body)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, target, strings.NewReader(string(b)))
	if key != "" {
		req.SetPathValue("key", key)
	}
	rec := httptest.NewRecorder()
	switch target {
	case "/api/agents":
		a.apiAgents(rec, req)
	case "/api/sessions/s1/resume":
		a.apiSessionResume(rec, req)
	default:
		t.Fatalf("no handler wired for %s", target)
	}
	return rec
}

// spawnReady drives the fake core through the events a fresh spawn
// produces: the terminal appears, pi files its first report, the
// bridge labels the session and the file lands.
func spawnReady(t *testing.T, f *hcFakeCore, sessPath string) {
	t.Helper()
	waitForHC(t, func() bool { return len(f.runLog()) == 1 })
	f.event("created", "t7", map[string]any{
		"address": "t7", "name": f.runLog()[0]["name"], "state": "live",
	})
	time.Sleep(30 * time.Millisecond)
	f.event("status", "t7", map[string]any{
		"status":   map[string]any{"state": "working", "app": "pi"},
		"reported": true,
	})
	if err := os.MkdirAll(filepath.Dir(sessPath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sessPath, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f.event("labels", "t7", map[string]any{
		"labels": map[string]any{"pi.session": sessPath},
	})
}

func TestHCAgentsCreateFlow(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)

	type createRes struct {
		code int
		body map[string]string
	}
	done := make(chan createRes, 1)
	go func() {
		rec := hcPost(t, a, "/api/agents", "", map[string]any{"cwd": proj, "prompt": "hello"})
		var body map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		done <- createRes{rec.Code, body}
	}()
	spawnReady(t, f, sessPath)

	select {
	case r := <-done:
		if r.code != 200 {
			t.Fatalf("create code = %d (%v), want 200", r.code, r.body)
		}
		if r.body["key"] != "s1" {
			t.Fatalf("create key = %q, want s1", r.body["key"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("create never answered")
	}

	runs := f.runLog()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	line, _ := runs[0]["line"].(string)
	if line != "pi" && !strings.HasPrefix(line, "pi ") {
		t.Fatalf("run line = %q, want a pi command", line)
	}
	if runs[0]["cwd"] != proj {
		t.Fatalf("run cwd = %v, want %s", runs[0]["cwd"], proj)
	}
	sends := f.sendLog()
	if len(sends) != 1 || sends[0]["text"] != "hello" {
		t.Fatalf("sends = %v, want one hello prompt", sends)
	}
}

func TestHCResumeSpawnsWithTheSessionFile(t *testing.T) {
	a, path := hcDialogApp(t)
	drv, f := startHCFake(t, path)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)

	type resumeRes struct {
		code int
		body map[string]string
	}
	done := make(chan resumeRes, 1)
	go func() {
		rec := hcPost(t, a, "/api/sessions/s1/resume", "s1", map[string]any{"text": "go"})
		var body map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &body)
		done <- resumeRes{rec.Code, body}
	}()
	spawnReady(t, f, path)

	select {
	case r := <-done:
		if r.code != 200 {
			t.Fatalf("resume code = %d (%v), want 200", r.code, r.body)
		}
		if r.body["key"] != "s1" {
			t.Fatalf("resume key = %q, want s1", r.body["key"])
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resume never answered")
	}

	runs := f.runLog()
	line, _ := runs[0]["line"].(string)
	if !strings.Contains(line, "--session") || !strings.Contains(line, path) {
		t.Fatalf("run line = %q, want --session %s", line, path)
	}
	sends := f.sendLog()
	if len(sends) != 1 || sends[0]["text"] != "go" {
		t.Fatalf("sends = %v, want one go prompt", sends)
	}
}

func TestHCWorkspacesListIsEmpty(t *testing.T) {
	a, _ := hcDialogApp(t)
	drv, _ := startHCFake(t, "/x/s1.jsonl")
	wireHC(a, drv)

	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	rec := httptest.NewRecorder()
	a.apiWorkspaces(rec, req)
	if rec.Code != 200 {
		t.Fatalf("workspaces code = %d, want 200", rec.Code)
	}
	var ws []map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &ws); err != nil || len(ws) != 0 {
		t.Fatalf("workspaces = %s, want []", rec.Body.String())
	}
}

func TestHCAgentsWorkspaceModeRefused(t *testing.T) {
	a, _ := hcDialogApp(t)
	drv, _ := startHCFake(t, "/x/s1.jsonl")
	wireHC(a, drv)

	rec := hcPost(t, a, "/api/agents", "", map[string]any{"workspace_id": "w1", "prompt": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("workspace-mode create code = %d, want 400", rec.Code)
	}
}
