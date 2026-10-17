package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/pi"
)

// pageFixtureApp chains five user messages so paging has depth.
func pageFixtureApp(t *testing.T) (*app, string, []pi.ViewItem) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	lines := []string{sessHeader}
	ids := []string{"a1", "a2", "a3", "a4", "a5"}
	prev := ""
	for i, id := range ids {
		lines = append(lines, userLine(id, prev, strings.Repeat("m", i+1)))
		prev = id
	}
	writeLines(t, path, lines...)

	w, err := pi.StartWatcher(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	a := &app{
		logger:  slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:     pi.NewIndex(root),
		watcher: w,
	}
	for range 50 {
		if _, ok := a.idx.Meta("s1"); ok {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	_, items, err := a.idx.Transcript("s1", 0)
	if err != nil || len(items) != 5 {
		t.Fatalf("fixture items: %d, %v", len(items), err)
	}
	return a, "s1", items
}

func getPage(t *testing.T, a *app, key, query string) (int, map[string]any) {
	t.Helper()
	h := a.guard(a.routes())
	req := httptest.NewRequest(http.MethodGet, "/api/sessions/"+key+"/transcript"+query, nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestTranscriptPageOlderThan(t *testing.T) {
	a, key, items := pageFixtureApp(t)
	last := strconv.Itoa(int(items[len(items)-1].Seq))

	code, body := getPage(t, a, key, "?before="+last+"&limit=2")
	if code != http.StatusOK {
		t.Fatalf("code %d body %v", code, body)
	}
	page := body["items"].([]any)
	if len(page) != 2 {
		t.Fatalf("page len %d", len(page))
	}
	first := page[0].(map[string]any)
	second := page[1].(map[string]any)
	// ascending, strictly older than the newest: a3, a4
	if int(first["seq"].(float64)) != int(items[2].Seq) || int(second["seq"].(float64)) != int(items[3].Seq) {
		t.Fatalf("page seqs: %v %v", first["seq"], second["seq"])
	}
	if body["more"] != true {
		t.Fatal("want more=true")
	}
}

func TestTranscriptPageOldestAndFull(t *testing.T) {
	a, key, items := pageFixtureApp(t)
	first := strconv.Itoa(int(items[0].Seq))
	code, body := getPage(t, a, key, "?before="+first)
	if code != http.StatusOK {
		t.Fatalf("code %d", code)
	}
	if n := len(body["items"].([]any)); n != 0 {
		t.Fatalf("oldest page items %d", n)
	}
	if body["more"] != false {
		t.Fatal("want more=false at the oldest page")
	}

	// default limit (20) caps at the whole branch and reports more=false
	code, body = getPage(t, a, key, "?before=999999")
	if code != http.StatusOK || body["more"] != false {
		t.Fatalf("full page: code %d more %v", code, body["more"])
	}
	if n := len(body["items"].([]any)); n != 5 {
		t.Fatalf("full page items %d", n)
	}
}

func TestTranscriptPageBadBefore(t *testing.T) {
	a, key, _ := pageFixtureApp(t)
	if code, _ := getPage(t, a, key, ""); code != http.StatusBadRequest {
		t.Fatalf("missing before: %d", code)
	}
	if code, _ := getPage(t, a, key, "?before=x"); code != http.StatusBadRequest {
		t.Fatalf("bad before: %d", code)
	}
	if code, _ := getPage(t, a, "nope", "?before=999999"); code != http.StatusNotFound {
		t.Fatalf("unknown key: %d", code)
	}
	if code, _ := getPage(t, a, key, "?before=999999&limit=x"); code != http.StatusBadRequest {
		t.Fatalf("bad limit: %d", code)
	}
}
