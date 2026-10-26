package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func postResume(t *testing.T, h http.Handler, key, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/"+key+"/resume", strings.NewReader(body))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestSessionResumeStartsAgentWithSessionFlag(t *testing.T) {
	var prompts, sends int32
	var lastPrompt string
	var lastArgs []string
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath:      &sessPath,
		prompts:       &prompts,
		sends:         &sends,
		lastPrompt:    &lastPrompt,
		lastStartArgs: &lastArgs,
		paneReady:     &piReadyScreen,
	})
	_, sub, h := streamTestAppWithHerdr(t, sock)
	// The fake's one agent drives s2, so the resume target s1 is
	// undriven and TargetForPath misses it.
	s2 := filepath.Join(sub, "s2.jsonl")
	writeLines(t, s2, sessHeader, userLine("b", "", "other"))
	sessPath = s2

	rec := postResume(t, h, "s1", `{"text":"hi again"}`)
	if rec.Code != 200 {
		t.Fatalf("resume = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if prompts != 1 {
		t.Fatalf("prompts = %d, want 1", prompts)
	}
	if lastPrompt == "" {
		t.Fatal("no agent.prompt target captured")
	}
	want := filepath.Join(sub, "s1.jsonl")
	if len(lastArgs) != 2 || lastArgs[0] != "--session" || lastArgs[1] != want {
		t.Fatalf("start args = %v, want [--session %s]", lastArgs, want)
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

func TestSessionResumeIdleSkipsPrompt(t *testing.T) {
	var prompts, sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath:  &sessPath,
		prompts:   &prompts,
		sends:     &sends,
		paneReady: &piReadyScreen,
	})
	_, sub, h := streamTestAppWithHerdr(t, sock)
	s2 := filepath.Join(sub, "s2.jsonl")
	writeLines(t, s2, sessHeader, userLine("b", "", "other"))
	sessPath = s2

	rec := postResume(t, h, "s1", `{}`)
	if rec.Code != 200 {
		t.Fatalf("resume = %d, want 200: %s", rec.Code, rec.Body.String())
	}
	if prompts != 0 {
		t.Fatalf("prompts = %d, want 0 for an idle resume", prompts)
	}
}

func TestSessionResumeUnknownKey404(t *testing.T) {
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath})
	_, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	rec := postResume(t, h, "nope", `{"text":"hi"}`)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("resume = %d, want 404", rec.Code)
	}
}

func TestSessionResumeRefusesDriven(t *testing.T) {
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath})
	_, sub, h := streamTestAppWithHerdr(t, sock)
	// The fake's agent drives s1 itself: the target is already driven.
	sessPath = filepath.Join(sub, "s1.jsonl")

	rec := postResume(t, h, "s1", `{"text":"hi"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("resume = %d, want 409", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "running") {
		t.Fatalf("body = %s, want a running error", rec.Body.String())
	}
}

func TestSessionResumeMissingCwdFallsBackHome(t *testing.T) {
	var prompts, sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath:  &sessPath,
		prompts:   &prompts,
		sends:     &sends,
		paneReady: &piReadyScreen,
	})
	_, sub, h := streamTestAppWithHerdr(t, sock)
	s2 := filepath.Join(sub, "s2.jsonl")
	writeLines(t, s2, sessHeader, userLine("b", "", "other"))
	sessPath = s2

	// s1's header points at /tmp/proj, which does not exist on this
	// machine; the resume must still succeed.
	rec := postResume(t, h, "s1", `{"text":"back"}`)
	if rec.Code != 200 {
		t.Fatalf("resume = %d, want 200: %s", rec.Code, rec.Body.String())
	}
}
