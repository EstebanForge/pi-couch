package server

import (
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestAPIItemFullText(t *testing.T) {
	_, sub, h := streamTestApp(t)
	path := filepath.Join(sub, "s1.jsonl")
	text := strings.Repeat("y", 1000)
	writeLines(t, path, `{"type":"message","id":"b","parentId":"a","timestamp":"2026-10-10T12:00:02.000Z","message":{"role":"toolResult","toolName":"read","content":[{"type":"text","text":"`+text+`"}],"timestamp":1760000002000}}`)

	code, body := getJSON(t, h, "/api/sessions/s1/items/1")
	if code != http.StatusOK {
		t.Fatalf("code = %d, body %v", code, body)
	}
	if got, want := body["text"], text; got != want {
		t.Fatalf("text len = %d, want %d", len(got.(string)), len(want))
	}

	code, _ = getJSON(t, h, "/api/sessions/s1/items/999")
	if code != http.StatusNotFound {
		t.Fatalf("unknown seq code = %d", code)
	}
	code, _ = getJSON(t, h, "/api/sessions/nope/items/0")
	if code != http.StatusNotFound {
		t.Fatalf("unknown key code = %d", code)
	}
	code, _ = getJSON(t, h, "/api/sessions/s1/items/notanumber")
	if code != http.StatusNotFound {
		t.Fatalf("bad seq code = %d", code)
	}
}
