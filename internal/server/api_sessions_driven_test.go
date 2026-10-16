package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
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
