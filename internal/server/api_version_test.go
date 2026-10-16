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
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	// The phone confirms deploys by eye: the shape is the contract.
	re := regexp.MustCompile(`^\d+\.\d+\.\d+\+[0-9A-Za-z.+-]+$`)
	if !re.MatchString(out.Version) {
		t.Fatalf("version %q is not semver with build metadata", out.Version)
	}
}
