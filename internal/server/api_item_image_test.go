package server

import (
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestItemImageUsesSafeMIMEAndNosniff(t *testing.T) {
	tests := []struct {
		mime string
		want string
	}{
		{mime: "image/png", want: "image/png"},
		{mime: "image/jpeg", want: "image/jpeg"},
		{mime: "image/gif", want: "image/gif"},
		{mime: "image/webp", want: "image/webp"},
		{mime: "image/bmp", want: "image/bmp"},
		{mime: "image/svg+xml", want: "application/octet-stream"},
		{mime: "text/html", want: "application/octet-stream"},
		{mime: "application/octet-stream", want: "application/octet-stream"},
		{mime: "", want: "application/octet-stream"},
	}
	for _, tc := range tests {
		t.Run(tc.mime, func(t *testing.T) {
			_, sub, h := streamTestApp(t)
			writeLines(t, filepath.Join(sub, "s1.jsonl"),
				`{"type":"message","id":"c","parentId":"a","timestamp":"2026-10-10T12:00:03.000Z","message":{"role":"toolResult","toolName":"read","content":[{"type":"image","data":"aGVsbG8=","mimeType":"`+tc.mime+`"}],"timestamp":1760000003000}}`,
			)
			req := httptest.NewRequest(http.MethodGet, "/api/sessions/s1/items/1/image", nil)
			req.Host = "127.0.0.1:8090"
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("got %d %s", rec.Code, rec.Body.String())
			}
			if got := rec.Header().Get("Content-Type"); got != tc.want {
				t.Errorf("Content-Type = %q, want %q", got, tc.want)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
	}
}

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
