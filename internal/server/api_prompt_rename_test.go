package server

import (
	"log/slog"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EstebanForge/couch-pi/internal/pi"
)

// pi's /name only runs as a command when the session is idle; queued
// into a busy pi it becomes steering text in the next turn. The
// "now" disposition (the chat rename) must refuse up front instead of
// answering ok and silently queueing.

func renameNowRequest(t *testing.T, a *app, key, text string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/sessions/"+key+"/prompt", strings.NewReader(`{"text":"`+text+`","whenBusy":"now"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	return rec
}

func renameTestApp(t *testing.T) (*app, *fakeStatus) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, filepath.Join(sub, "s1.jsonl"), sessHeader, userLine("a", "", "first"))
	fake := &fakeStatus{badges: map[string]string{}, targets: map[string]string{}}
	a := &app{
		logger:      slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:         pi.NewIndex(root),
		home:        testHome(),
		answered:    newDialogMemos(),
		models:      newModelsCache(t.TempDir() + "/models.json"),
		memory:      newMemoryProbe("", ""),
		status:      fake,
		dialogLatch: map[string]*dialogWire{},
	}
	return a, fake
}

func TestPromptRefusesRenameNowWhileBusy(t *testing.T) {
	a, fake := renameTestApp(t)
	meta, ok := a.idx.Meta("s1")
	if !ok {
		t.Fatal("session s1 not indexed")
	}
	fake.mu.Lock()
	fake.badges[meta.Path] = "working"
	fake.mu.Unlock()
	rec := renameNowRequest(t, a, "s1", "/name Probe")
	if rec.Code != 409 || !strings.Contains(rec.Body.String(), "busy") {
		t.Fatalf("busy rename now = %d %s, want 409 busy", rec.Code, rec.Body.String())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.cmds) != 0 {
		t.Fatalf("commands = %v, want no prompt dispatched", fake.cmds)
	}
}

func TestPromptRenameNowOnIdleSessionSubmits(t *testing.T) {
	a, fake := renameTestApp(t)
	meta, ok := a.idx.Meta("s1")
	if !ok {
		t.Fatal("session s1 not indexed")
	}
	fake.mu.Lock()
	fake.badges[meta.Path] = "done"
	fake.targets[meta.Path] = "t1"
	fake.mu.Unlock()
	rec := renameNowRequest(t, a, "s1", "/name Probe")
	if rec.Code != 200 || !strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("idle rename now = %d %s, want 200 ok", rec.Code, rec.Body.String())
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.cmds) != 1 {
		t.Fatalf("commands = %v, want the rename prompt dispatched", fake.cmds)
	}
}

func TestPromptRenameNowWithUnknownStatusSubmits(t *testing.T) {
	a, fake := renameTestApp(t)
	meta, ok := a.idx.Meta("s1")
	if !ok {
		t.Fatal("session s1 not indexed")
	}
	fake.mu.Lock()
	fake.targets[meta.Path] = "t1"
	fake.mu.Unlock()
	rec := renameNowRequest(t, a, "s1", "/name Probe")
	if rec.Code != 200 {
		t.Fatalf("unknown-status rename now = %d %s, want 200 (herdr gates its own prompts)", rec.Code, rec.Body.String())
	}
}

func TestPromptSteeringStillQueuesWhileBusy(t *testing.T) {
	a, fake := renameTestApp(t)
	meta, ok := a.idx.Meta("s1")
	if !ok {
		t.Fatal("session s1 not indexed")
	}
	fake.mu.Lock()
	fake.badges[meta.Path] = "working"
	fake.targets[meta.Path] = "t1"
	fake.mu.Unlock()
	req := httptest.NewRequest("POST", "/api/sessions/s1/prompt", strings.NewReader(`{"text":"look at X","whenBusy":"followUp"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.guard(a.routes()).ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("steering while busy = %d %s, want 200 (queueing stays legal)", rec.Code, rec.Body.String())
	}
}
