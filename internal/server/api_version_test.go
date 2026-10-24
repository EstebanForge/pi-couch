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
	// Build metadata is optional now that the version is a plain release.
	re := regexp.MustCompile(`^\d+\.\d+\.\d+(\+[0-9A-Za-z.+-]+)?$`)
	if !re.MatchString(out.Version) {
		t.Fatalf("version %q is not semver", out.Version)
	}
}
