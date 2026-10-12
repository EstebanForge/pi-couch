package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONPrompt(t *testing.T) {
	a, sub, h := streamTestApp(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Empty text: 400, before anything else.
	resp, err := http.Post(srv.URL+"/api/sessions/nope/prompt", "", strings.NewReader(`{"text":"   "}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty text: %d, want 400", resp.StatusCode)
	}

	// Unknown key: 404 notfound.
	resp, err = http.Post(srv.URL+"/api/sessions/nope/prompt", "", strings.NewReader(`{"text":"hi"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown key: %d, want 404", resp.StatusCode)
	}

	// Known key, no herdr agent drives the fixture path: 409 notdriven,
	// deterministic like the stop and close twins.
	writeLines(t, filepath.Join(sub, "prompt-key.jsonl"), sessHeader, userLine("a", "", "first"))
	resp, err = http.Post(srv.URL+"/api/sessions/prompt-key/prompt", "", strings.NewReader(`{"text":"hello agent"}`))
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("no driver: %d, want 409", resp.StatusCode)
	}
	var out struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatal(err)
	}
	if out.Error != "notdriven" {
		t.Fatalf("error=%q, want notdriven", out.Error)
	}

	// Garbage body: 400.
	resp, err = http.Post(srv.URL+"/api/sessions/prompt-key/prompt", "", strings.NewReader(`{`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("garbage body: %d, want 400", resp.StatusCode)
	}
	_ = a
}
