package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestAPIAgentsRejectsBadBody(t *testing.T) {
	a := &app{logger: testLogger()}
	req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader("{bad"))
	req.Host = "127.0.0.1:8090"
	w := httptest.NewRecorder()
	a.apiAgents(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400", w.Code)
	}
}

func TestAPIAgentsRejectsMissingPromptOrCwd(t *testing.T) {
	a := &app{logger: testLogger()}
	for _, body := range []string{
		`{"cwd":"/tmp/x"}`,
		`{"prompt":"hi"}`,
		`{"cwd":"relative/path","prompt":"hi"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		w := httptest.NewRecorder()
		a.apiAgents(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("body %s: code %d, want 400", body, w.Code)
		}
	}
}

func TestAPIAgentsRejectsOutsideHome(t *testing.T) {
	a := &app{logger: testLogger()}
	// /etc is a real dir outside home; the jail must refuse it before
	// any herdr call.
	req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(`{"cwd":"/etc","prompt":"hi"}`))
	req.Host = "127.0.0.1:8090"
	w := httptest.NewRecorder()
	a.apiAgents(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code %d, want 400", w.Code)
	}
}

func TestAgentNameFoldsFolderNames(t *testing.T) {
	for in, want := range map[string]string{
		".cache":          "cache",
		"couchpi-fab-e2e": "couchpi-fab-e2e",
		"My Project.v2":   "my-project-v2",
		"-_weird-":        "weird",
		"verylongfoldername-abcdefghijklmnopqrstuvwxyz0123456789": "verylongfoldername-abcdefghijk",
	} {
		if got := agentName(in); got != want {
			t.Errorf("agentName(%q) = %q, want %q", in, got, want)
		}
	}
}
