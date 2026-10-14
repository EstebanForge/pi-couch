package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(testDiscard{}, nil))
}

func TestBridgeDialogSetAndClear(t *testing.T) {
	a := &app{logger: testLogger()}
	req := httptest.NewRequest(http.MethodPost, "/api/bridge/dialog", strings.NewReader(
		`{"phase":"start","kind":"editor","title":"Commit message","session_path":"/s/a.jsonl"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiBridgeDialog(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("start: code=%d, want 204", rec.Code)
	}

	d, ok := a.bridgeDialog("/s/a.jsonl")
	if !ok || d.Kind != "editor" || d.Title != "Commit message" {
		t.Fatalf("bridgeDialog = %+v ok=%v", d, ok)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/bridge/dialog", strings.NewReader(
		`{"phase":"end","session_path":"/s/a.jsonl"}`))
	req.Host = "127.0.0.1:8090"
	rec = httptest.NewRecorder()
	a.apiBridgeDialog(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("end: code=%d, want 204", rec.Code)
	}
	if _, ok := a.bridgeDialog("/s/a.jsonl"); ok {
		t.Error("dialog should be gone after phase=end")
	}
}

func TestBridgeDialogExpires(t *testing.T) {
	a := &app{logger: testLogger()}
	a.bridgeMu.Lock()
	if a.bridge == nil {
		a.bridge = map[string]bridgeEntry{}
	}
	a.bridge["/s/a.jsonl"] = bridgeEntry{dialog: BridgeDialog{Kind: "select", Title: "Pick"}, expires: time.Now().Add(-time.Second)}
	a.bridgeMu.Unlock()

	if _, ok := a.bridgeDialog("/s/a.jsonl"); ok {
		t.Error("expired dialog must not surface")
	}
}

func TestBridgeDialogBadBody(t *testing.T) {
	a := &app{logger: testLogger()}
	req := httptest.NewRequest(http.MethodPost, "/api/bridge/dialog", strings.NewReader(`{`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiBridgeDialog(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code=%d, want 400", rec.Code)
	}
}
