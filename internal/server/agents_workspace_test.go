package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// The two new create modes drive herdr against an existing workspace:
// {workspace_id} splits a pane there, {label,cwd} reuses or creates
// the labeled workspace (the Quick flow).
// runAgent retries agent.start while herdr reports the pane shell not
// yet available: a fresh workspace's root shell takes a moment to
// spawn, and the bind is otherwise a race.
func TestAPIAgentsRetriesShellRace(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, failFirstStart: true})
	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")
	a, _, _ := streamTestAppWithHerdr(t, sock)
	req := httptest.NewRequest(http.MethodPost, "/api/agents",
		strings.NewReader(`{"workspace_id":"w1","prompt":"hi"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiAgents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
}

func TestAPIAgentsInWorkspace(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends})
	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")

	t.Run("workspace_id splits and starts", func(t *testing.T) {
		a, _, _ := streamTestAppWithHerdr(t, sock)
		req := httptest.NewRequest(http.MethodPost, "/api/agents",
			strings.NewReader(`{"workspace_id":"w1","prompt":"hi"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.apiAgents(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})

	t.Run("label reuses the existing workspace", func(t *testing.T) {
		var sends2 int32
		sess2 := filepath.Join(t.TempDir(), "s1.jsonl")
		sock2 := herdrFake(t, fakeHerdrOpts{sessPath: &sess2, sends: &sends2})
		a, _, _ := streamTestAppWithHerdr(t, sock2)
		// A real dir under home: the jail checks it before any herdr
		// call, so /tmp won't do.
		home, err := os.UserHomeDir()
		if err != nil {
			t.Fatal(err)
		}
		dir, err := os.MkdirTemp(home, "couchpi-fab-*")
		if err != nil {
			t.Fatal(err)
		}
		defer os.RemoveAll(dir)
		req := httptest.NewRequest(http.MethodPost, "/api/agents",
			strings.NewReader(`{"label":"Forge","cwd":"`+dir+`","prompt":"go"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.apiAgents(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
		}
	})
}

// The Quick flow starts pi with no prompt and answers {key:"",
// agent:<name>}: the phone lands in a pending chat that prompts the
// agent by name until pi writes its session file.
func TestAPIAgentsQuickPromptless(t *testing.T) {
	var sends, prompts int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, prompts: &prompts})
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, "couchpi-fab-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	a, _, _ := streamTestAppWithHerdr(t, sock)
	req := httptest.NewRequest(http.MethodPost, "/api/agents",
		strings.NewReader(`{"label":"Forge","cwd":"`+dir+`"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiAgents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	var resp struct {
		Key   string `json:"key"`
		Agent string `json:"agent"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatal(err)
	}
	if resp.Key != "" {
		t.Errorf("key=%q, want empty", resp.Key)
	}
	if resp.Agent == "" {
		t.Error("agent name missing from the response")
	}
	if n := atomic.LoadInt32(&prompts); n != 0 {
		t.Errorf("agent.prompt fired %d times, want 0", n)
	}
}

// The pending chat's pair: prompt by agent name lands as agent.prompt,
// and the session lookup answers the key only once herdr carries a
// session path for that agent.
func TestAgentPendingEndpoints(t *testing.T) {
	var sends, prompts int32
	lastPrompt := ""
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, prompts: &prompts, lastPrompt: &lastPrompt})
	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")
	a, _, _ := streamTestAppWithHerdr(t, sock)

	req := httptest.NewRequest(http.MethodPost, "/api/agents/quick-ab/prompt",
		strings.NewReader(`{"text":"hi there"}`))
	req.Host = "127.0.0.1:8090"
	req.SetPathValue("name", "quick-ab")
	rec := httptest.NewRecorder()
	a.apiAgentPrompt(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("prompt code=%d body=%s", rec.Code, rec.Body.String())
	}
	if lastPrompt != "quick-ab" {
		t.Errorf("prompt targeted %q, want quick-ab", lastPrompt)
	}

	req = httptest.NewRequest(http.MethodGet, "/api/agents/pi/session", nil)
	req.Host = "127.0.0.1:8090"
	req.SetPathValue("name", "pi")
	rec = httptest.NewRecorder()
	a.apiAgentSession(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("session code=%d", rec.Code)
	}
	var got struct {
		Key string `json:"key"`
	}
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	wantKey := strings.TrimSuffix(filepath.Base(sessPath), ".jsonl")
	if got.Key != wantKey {
		t.Errorf("key=%q, want %q", got.Key, wantKey)
	}
}

func TestAgentPendingNameValidation(t *testing.T) {
	a := &app{logger: testLogger()}
	for _, name := range []string{"Bad", "with space", "", "toolongtoolongtoolongtoolongtoolong"} {
		req := httptest.NewRequest(http.MethodPost, "/api/agents/x/prompt", strings.NewReader(`{"text":"hi"}`))
		req.Host = "127.0.0.1:8090"
		req.SetPathValue("name", name)
		rec := httptest.NewRecorder()
		a.apiAgentPrompt(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("name %q: code=%d, want 400", name, rec.Code)
		}
	}
}

func TestAPIAgentsRejectsAmbiguousSelector(t *testing.T) {
	a := &app{logger: testLogger()}
	for _, body := range []string{
		`{"workspace_id":"w1","cwd":"/tmp","prompt":"hi"}`,
		`{"workspace_id":"w1","label":"x","prompt":"hi"}`,
		`{"label":"x","cwd":"/tmp","workspace_id":"w1","prompt":"hi"}`,
		`{"workspace_id":"w1","label":"x","cwd":"/tmp","prompt":"hi"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.apiAgents(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: code=%d, want 400", body, rec.Code)
		}
	}
}

func TestWorkspacesEndpoint(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends})
	a, _, h := streamTestAppWithHerdr(t, sock)
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), `"label":"Forge"`) {
		t.Fatalf("body=%s", rec.Body.String())
	}
	_ = a
}
