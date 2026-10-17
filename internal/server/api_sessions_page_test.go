package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

// GET /api/sessions serves a window, not the whole list: limit caps the
// rows, q filters name/first_user/cwd, and total always reports the
// matched count so the phone can offer load-more.
func TestSessionsLimitQueryTotal(t *testing.T) {
	_, sub, h := streamTestApp(t)
	writeLines(t, filepath.Join(sub, "s2.jsonl"), sessHeader, userLine("u2", "", "needle in first message"))
	writeLines(t, filepath.Join(sub, "s3.jsonl"), sessHeader, userLine("u3", "", "unrelated prose"))

	get := func(url string) (int, sessionsWire) {
		req := httptest.NewRequest(http.MethodGet, url, nil)
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			return rec.Code, sessionsWire{}
		}
		var out sessionsWire
		if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
			t.Fatalf("decode %s: %v", rec.Body.String(), err)
		}
		return rec.Code, out
	}

	code, out := get("/api/sessions")
	if code != http.StatusOK || len(out.Sessions) != 3 || out.Total != 3 {
		t.Fatalf("whole list: code=%d n=%d total=%d", code, len(out.Sessions), out.Total)
	}
	if out.Sessions[0].Key != "s3" {
		t.Fatalf("order: first=%s, want s3 (newest mtime)", out.Sessions[0].Key)
	}

	code, out = get("/api/sessions?limit=2")
	if code != http.StatusOK || len(out.Sessions) != 2 || out.Total != 3 {
		t.Fatalf("limit: code=%d n=%d total=%d", code, len(out.Sessions), out.Total)
	}
	if out.Sessions[0].Key != "s3" {
		t.Fatalf("limit order: first=%s", out.Sessions[0].Key)
	}

	code, out = get("/api/sessions?q=needle")
	if code != http.StatusOK || len(out.Sessions) != 1 || out.Total != 1 {
		t.Fatalf("q first_user: code=%d n=%d total=%d", code, len(out.Sessions), out.Total)
	}
	if out.Sessions[0].Key != "s2" {
		t.Fatalf("q match: key=%s", out.Sessions[0].Key)
	}

	code, out = get("/api/sessions?q=PROJ")
	if code != http.StatusOK || out.Total != 3 {
		t.Fatalf("q cwd is case-insensitive: code=%d total=%d", code, out.Total)
	}

	if code, _ := get("/api/sessions?limit=abc"); code != http.StatusBadRequest {
		t.Fatalf("garbage limit: %d", code)
	}
	if code, _ := get("/api/sessions?limit=0"); code != http.StatusBadRequest {
		t.Fatalf("zero limit: %d", code)
	}
}
