package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"regexp"
	"testing"
)

func TestVersionEndpoint(t *testing.T) {
	_, _, h := streamTestApp(t)
	req := httptest.NewRequest(http.MethodGet, "/api/version", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d, want 200", rec.Code)
	}
	var out struct {
		Version string `json:"version"`
		Home    string `json:"home"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The client tildifies paths against the server's home; without it
	// every card shows raw absolute paths.
	if out.Home == "" {
		t.Fatal("home missing from /api/version")
	}
	// The phone confirms deploys by eye: the shape is the contract.
	// Date-based releases: YYYYMMDD, same-day re-releases .1, .2, ...
	re := regexp.MustCompile(`^\d{8}(\.\d+)?$`)
	if !re.MatchString(out.Version) {
		t.Fatalf("version %q is not a date version", out.Version)
	}
}
