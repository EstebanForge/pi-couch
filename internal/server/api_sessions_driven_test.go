package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"
)

// The home cards render the live driver state: the pane title and the
// workspace label ride next to the status, joined by session path.
func TestSessionsCarriesDrivenInfo(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends})
	a, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d, want 200", rec.Code)
	}
	var out sessionsWire
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d, ok := out.Driven[sessPath]
	if !ok {
		t.Fatalf("no driven entry for %s", sessPath)
	}
	if d.Status != "blocked" {
		t.Errorf("status=%q, want blocked", d.Status)
	}
	if d.Title != "pi - s1:running..." {
		t.Errorf("title=%q", d.Title)
	}
	if d.Label != "Forge" {
		t.Errorf("label=%q", d.Label)
	}
	_ = sub
	_ = a
}

// Field-delta frames: the sessions stream resends a field only when it
// changed, so a file tick without driver movement omits the herdr maps.
func TestSessionsStreamFieldDelta(t *testing.T) {
	var sends int32
	var sessPath string
	status := "blocked"
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, status: &status})
	a, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stream/sessions")
	if err != nil {
		t.Fatal(err)
	}
	frames := readFrames(t, resp.Body)
	defer resp.Body.Close()

	keys := func(f frame) map[string]bool {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(f.data), &m); err != nil {
			t.Fatalf("decode %s: %v", f.data, err)
		}
		out := map[string]bool{}
		for k := range m {
			out[k] = true
		}
		return out
	}

	f := waitFrame(t, frames, 5*time.Second, "reset")
	k := keys(f)
	for _, want := range []string{"sessions", "badges", "driven", "awaiting"} {
		if !k[want] {
			t.Fatalf("first frame missing %s: %s", want, f.data)
		}
	}

	// A file tick with no driver movement: sessions only.
	writeLines(t, filepath.Join(sub, "s2.jsonl"), sessHeader)
	f = waitFrame(t, frames, 8*time.Second, "reset")
	k = keys(f)
	if !k["sessions"] {
		t.Fatalf("second frame missing sessions: %s", f.data)
	}
	for _, gone := range []string{"badges", "driven", "awaiting"} {
		if k[gone] {
			t.Fatalf("second frame resent %s: %s", gone, f.data)
		}
	}
	var snap sessionsWire
	if err := json.Unmarshal([]byte(f.data), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Sessions) != 2 {
		t.Fatalf("second frame sessions=%d, want 2", len(snap.Sessions))
	}

	// A driver status flip rides the next file tick: badges and driven
	// come back, awaiting stays out.
	status = "working"
	writeLines(t, filepath.Join(sub, "s3.jsonl"), sessHeader)
	f = waitFrame(t, frames, 8*time.Second, "reset")
	k = keys(f)
	if !k["badges"] || !k["driven"] {
		t.Fatalf("third frame missing driver fields: %s", f.data)
	}
	if k["awaiting"] {
		t.Fatalf("third frame resent awaiting: %s", f.data)
	}
	_ = a
}
