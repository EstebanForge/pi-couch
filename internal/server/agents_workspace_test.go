package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// The two new create modes drive herdr against an existing workspace:
// {workspace_id} splits a pane there, {label,cwd} reuses or creates
// the labeled workspace (the Quick flow).
// runAgent retries agent.start while herdr reports the pane shell not
// yet available: a fresh workspace's root shell takes a moment to
// spawn, and the bind is otherwise a race.
// piReadyScreen carries the stats footer parse: the marker the create
// waits for before prompting.
var piReadyScreen = "pi ready\n~/Dev\n0.0%/1.0M glm-5.3-flash\n"

func TestAPIAgentsRetriesShellRace(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneReady: &piReadyScreen, failFirstStart: true})

	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")
	// The create verifies the file, not just herdr's early path.
	if err := os.WriteFile(sessPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
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
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneReady: &piReadyScreen})

	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")
	// The create verifies the file, not just herdr's early path.
	if err := os.WriteFile(sessPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}

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
		if err := os.WriteFile(sess2, []byte("{}\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		sock2 := herdrFake(t, fakeHerdrOpts{sessPath: &sess2, sends: &sends2, paneReady: &piReadyScreen})
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

// Any create without a prompt is a refusal: pi writes the session
// file only at turn start, so a promptless agent could never surface
// on the home list.
func TestAPIAgentsRequirePrompt(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, "couchpi-fab-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	a := &app{logger: testLogger()}
	for _, body := range []string{
		`{"cwd":"` + dir + `"}`,
		`{"workspace_id":"w1"}`,
		`{"label":"x","cwd":"` + dir + `","prompt":"   "}`,
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

// seedModelsFile primes the models cache file so create-time model
// validation has a catalog to match against.
func seedModelsFile(t *testing.T, a *app, ids ...string) {
	t.Helper()
	rows := make([]modelRow, len(ids))
	for i, id := range ids {
		rows[i] = modelRow{ID: id}
	}
	b, err := json.Marshal(map[string]any{
		"fetched_at": time.Now().UTC(),
		"rows":       rows,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.models.path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The create panel can pin a model and thinking level; they ride
// agent.start args as one pi --model flag.
func TestAPIAgentsCarriesModelArgs(t *testing.T) {
	var sends int32
	var sessPath string
	var startArgs []string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, paneReady: &piReadyScreen, lastStartArgs: &startArgs})

	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")
	// The create verifies the file, not just herdr's early path.
	if err := os.WriteFile(sessPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _, _ := streamTestAppWithHerdr(t, sock)
	seedModelsFile(t, a, "zai/glm-5.3-flash", "zai/glm-5.3")
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
		strings.NewReader(`{"label":"Forge","cwd":"`+dir+`","prompt":"go","model":"zai/glm-5.3-flash","thinking":"high"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiAgents(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
	}
	want := []string{"--model", "zai/glm-5.3-flash:high"}
	if len(startArgs) != len(want) || startArgs[0] != want[0] || startArgs[1] != want[1] {
		t.Fatalf("start args=%v want=%v", startArgs, want)
	}
}

// The prompt can fail soft — herdr accepts it while pi settles and
// the keys go nowhere. When the draft sits in the editor the recovery
// submits it with one enter; when the prompt was lost it re-prompts.
func TestEnsureTurnStartedSubmitsDraft(t *testing.T) {
	oldWait := sessionVerifyWait
	sessionVerifyWait = 300 * time.Millisecond
	t.Cleanup(func() { sessionVerifyWait = oldWait })
	var sends, prompts, ents int32
	var sessPath string
	onEnter := func() {
		atomic.AddInt32(&ents, 1)
		// The corrective enter submits the sitting draft; pi starts
		// the turn and the session file lands.
		if err := os.WriteFile(sessPath, []byte("{}\n"), 0o644); err != nil {
			t.Errorf("fixture write: %v", err)
		}
	}
	// Pane: the draft "go" sits in the editor.
	draft := "editor\ngo\n0.0%/1.0M\n"
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, prompts: &prompts, paneText: &draft, onEnter: onEnter})
	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")
	a, _, _ := streamTestAppWithHerdr(t, sock)
	a.ensureTurnStarted(context.Background(), "p1", "quick-go", "go")
	if n := atomic.LoadInt32(&ents); n != 1 {
		t.Errorf("corrective enters = %d, want 1", n)
	}
	if n := atomic.LoadInt32(&sends); n != 1 {
		t.Errorf("send_input calls = %d, want 1", n)
	}
	if n := atomic.LoadInt32(&prompts); n != 0 {
		t.Errorf("agent.prompt calls = %d, want 0 (runAgent prompted already)", n)
	}
}

// A waiting herdr queue delivers the first prompt eventually, so a
// re-prompt would duplicate the turn. A lost-looking prompt must
// never be sent twice.
func TestEnsureTurnStartedNeverReprompts(t *testing.T) {
	oldWait := sessionVerifyWait
	sessionVerifyWait = 300 * time.Millisecond
	t.Cleanup(func() { sessionVerifyWait = oldWait })
	var sends, prompts int32
	var sessPath string
	// Pane without the draft: the prompt looks lost, but re-prompting
	// is still forbidden.
	empty := "pi ready\nnobody home\n"
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, prompts: &prompts, paneText: &empty})
	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")
	a, _, _ := streamTestAppWithHerdr(t, sock)
	a.ensureTurnStarted(context.Background(), "p1", "quick-x", "tell me a story")
	if n := atomic.LoadInt32(&prompts); n != 0 {
		t.Errorf("agent.prompt calls = %d, want 0 (never re-prompt)", n)
	}
	if n := atomic.LoadInt32(&sends); n != 0 {
		t.Errorf("send_input calls = %d, want 0", n)
	}
}

// Bad model or thinking picks must 400 before any herdr call: a bad
// pi flag exits pi and the create would otherwise time out waiting
// for an agent that never binds.
func TestAPIAgentsRejectsBadModelThinking(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends})

	sessPath = filepath.Join(t.TempDir(), "s1.jsonl")
	// The create verifies the file, not just herdr's early path.
	if err := os.WriteFile(sessPath, []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	a, _, _ := streamTestAppWithHerdr(t, sock)
	seedModelsFile(t, a, "zai/glm-5.3-flash")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, "couchpi-fab-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)

	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown model", `{"label":"Forge","cwd":"` + dir + `","prompt":"go","model":"nope/x","thinking":"high"}`, "unknown model"},
		{"bad thinking", `{"label":"Forge","cwd":"` + dir + `","prompt":"go","model":"zai/glm-5.3-flash","thinking":"turbo"}`, "bad thinking"},
		{"thinking without model", `{"label":"Forge","cwd":"` + dir + `","prompt":"go","thinking":"high"}`, "thinking requires a model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(tc.body))
			req.Host = "127.0.0.1:8090"
			rec := httptest.NewRecorder()
			a.apiAgents(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body=%s want substring %s", rec.Body.String(), tc.want)
			}
		})
	}
}
