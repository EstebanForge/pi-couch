package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestItemServesImage(t *testing.T) {
	a, sub, h := streamTestApp(t)
	_ = a
	// Fixture: user text (seq 0) then a read result with an image (seq 1).
	writeLines(t, filepath.Join(sub, "s1.jsonl"),
		`{"type":"message","id":"c","parentId":"a","timestamp":"2026-10-10T12:00:03.000Z","message":{"role":"toolResult","toolName":"read","content":[{"type":"text","text":"[image read]"},{"type":"image","data":"aGVsbG8=","mimeType":"image/png"}],"timestamp":1760000003000}}`,
	)

	get := func(seq string) *httptest.ResponseRecorder {
		req := httptest.NewRequest("GET", "/api/sessions/s1/items/"+seq+"/image", nil)
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	rec := get("1")
	if rec.Code != http.StatusOK {
		t.Fatalf("image: got %d %s", rec.Code, rec.Body.String())
	}
	if got := rec.Body.String(); got != "hello" {
		t.Errorf("body = %q, want decoded payload", got)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "image/png" {
		t.Errorf("Content-Type = %q", ct)
	}

	if rec = get("0"); rec.Code != http.StatusNotFound {
		t.Errorf("text-only: got %d, want 404", rec.Code)
	} else if len(rec.Body.String()) > 0 && rec.Body.String()[0] == '{' {
		t.Errorf("text-only body = %q, want plain", rec.Body.String())
	}
	if rec = get("zz"); rec.Code != http.StatusNotFound {
		t.Errorf("bad seq: got %d, want 404", rec.Code)
	}
}
