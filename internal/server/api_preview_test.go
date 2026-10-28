package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode/utf8"
)

// previewApp chains seven more user messages onto the harness session
// (s1 already holds one), the real parent-linked jsonl shape.
func previewApp(t *testing.T) (*app, string, http.Handler) {
	t.Helper()
	a, sub, h := streamTestApp(t)
	sessPath := sub + "/s1.jsonl"
	lines := make([]string, 0, 7)
	prev := "a"
	for i := 1; i <= 7; i++ {
		id := string(rune('b' + i - 1))
		lines = append(lines, userLine(id, prev, "msg"+string(rune('0'+i))))
		prev = id
	}
	writeLines(t, sessPath, lines...)
	return a, sessPath, h
}

func getPreview(t *testing.T, h http.Handler, key string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+key+"/preview", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out map[string]any
	if rec.Code == http.StatusOK {
		if json.Unmarshal(rec.Body.Bytes(), &out) != nil {
			t.Fatalf("preview body is not JSON: %s", rec.Body.String())
		}
	}
	return rec.Code, out
}

// The preview is the last few conversation lines, newest last, each
// capped at the render path's 400-rune budget.
func TestAPIPreviewCapsLines(t *testing.T) {
	_, sessPath, h := previewApp(t)
	writeLines(t, sessPath, userLine("zz", "h", strings.Repeat("long ", 120)))
	code, out := getPreview(t, h, "s1")
	if code != http.StatusOK {
		t.Fatalf("preview: code=%d, want 200", code)
	}
	lines, _ := out["lines"].([]any)
	if len(lines) != 6 {
		t.Fatalf("lines=%d, want 6", len(lines))
	}
	for i, l := range lines {
		m := l.(map[string]any)
		if utf8.RuneCountInString(m["text"].(string)) > 400 {
			t.Fatalf("line %d exceeds the 400-rune cap", i)
		}
		if _, ok := m["seq"].(float64); !ok {
			t.Fatalf("line %d has no numeric seq", i)
		}
	}
	// Newest last: the final line is the long message as the preview
	// cap shaped it - 399 runes of the "long " pattern, then the ellipsis.
	last := lines[5].(map[string]any)
	want := strings.Repeat("long ", 79) + "long…"
	if got := last["text"].(string); got != want {
		t.Fatalf("last line is not the capped newest message: len=%d want len=%d", len([]rune(got)), len([]rune(want)))
	}
	if last["who"] != "user" {
		t.Fatalf("last line who=%v, want user", last["who"])
	}
}

// A session with no conversation answers 200 with an empty JSON array,
// never null and never an error: the card exists, the thread is just
// silent.
func TestAPIPreviewEmptyThread(t *testing.T) {
	_, sub, h := streamTestApp(t)
	writeLines(t, sub+"/s2.jsonl", sessHeader)
	code, out := getPreview(t, h, "s2")
	if code != http.StatusOK {
		t.Fatalf("preview: code=%d, want 200", code)
	}
	lines, _ := out["lines"].([]any)
	if lines == nil || len(lines) != 0 {
		t.Fatalf("lines=%v, want empty non-null array", out["lines"])
	}
}

// Status comes from the herdr badges the card shows, not the index.
func TestAPIPreviewStatusFromDriveStatus(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends})
	_, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = sub + "/s1.jsonl"
	code, out := getPreview(t, h, "s1")
	if code != http.StatusOK {
		t.Fatalf("preview: code=%d, want 200", code)
	}
	if out["status"] != "blocked" {
		t.Fatalf("status=%v, want the fake's blocked badge", out["status"])
	}
	if title, _ := out["title"].(string); title == "" {
		t.Fatalf("title missing: %v", out)
	}
	if awaiting, ok := out["awaiting"].(bool); !ok || awaiting {
		t.Fatalf("awaiting=%v, want false", out["awaiting"])
	}
	lines, _ := out["lines"].([]any)
	if len(lines) != 1 {
		t.Fatalf("lines=%d, want the single seeded user message", len(lines))
	}
}

func TestAPIPreviewUnknownKey(t *testing.T) {
	_, _, h := streamTestApp(t)
	code, _ := getPreview(t, h, "nope")
	if code != http.StatusNotFound {
		t.Fatalf("unknown key: code=%d, want 404", code)
	}
}
