package server

import (
	"context"
	"io"
	"net/http/httptest"
	"testing"

	"github.com/a-h/templ"
)

func TestServeTemplNoCache(t *testing.T) {
	c := templ.ComponentFunc(func(ctx context.Context, w io.Writer) error {
		_, err := io.WriteString(w, "<p>x</p>")
		return err
	})
	rec := httptest.NewRecorder()
	serveTempl(rec, httptest.NewRequest("GET", "/x", nil), c)
	if got := rec.Header().Get("Cache-Control"); got != "no-cache" {
		t.Fatalf("Cache-Control=%q, want no-cache", got)
	}
}
