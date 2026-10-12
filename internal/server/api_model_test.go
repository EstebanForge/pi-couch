package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
)

func TestJSONModelThinking(t *testing.T) {
	a, sub, h := streamTestApp(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// Bad pattern / bad level: 400, before session lookup.
	for _, tc := range []struct{ route, body string }{
		{"/api/sessions/nope/model", `{"pattern":"has\nnewline"}`},
		{"/api/sessions/nope/thinking", `{"level":"ultra"}`},
	} {
		resp, err := http.Post(srv.URL+tc.route, "", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusBadRequest {
			t.Fatalf("%s: %d, want 400", tc.route, resp.StatusCode)
		}
	}

	// Unknown key: 404.
	for _, tc := range []struct{ route, body string }{
		{"/api/sessions/nope/model", `{"pattern":"zai/glm"}`},
		{"/api/sessions/nope/thinking", `{"level":"high"}`},
	} {
		resp, err := http.Post(srv.URL+tc.route, "", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("%s: %d, want 404", tc.route, resp.StatusCode)
		}
	}

	// Known key, no driver: 409 in-band.
	writeLines(t, filepath.Join(sub, "mt-key.jsonl"), sessHeader, userLine("a", "", "first"))
	for _, tc := range []struct{ route, body, want string }{
		{"/api/sessions/mt-key/model", `{"pattern":"zai/glm"}`, "notdriven"},
		{"/api/sessions/mt-key/thinking", `{"level":"high"}`, "notdriven"},
	} {
		resp, err := http.Post(srv.URL+tc.route, "", strings.NewReader(tc.body))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%s: %d, want 409", tc.route, resp.StatusCode)
		}
		var out struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if out.Error != tc.want {
			t.Fatalf("%s: error=%q, want %q", tc.route, out.Error, tc.want)
		}
	}
	_ = a
}
