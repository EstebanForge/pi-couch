package pi

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"sync/atomic"
	"time"
)

func testIndex(t *testing.T) *Index {
	t.Helper()
	root := t.TempDir()
	src, err := os.ReadFile(filepath.Join("testdata", "fixture.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "--tmp-proj--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "20261010_fixture.jsonl"), src, 0o644); err != nil {
		t.Fatal(err)
	}
	return NewIndex(root)
}

func TestListSortedAndCached(t *testing.T) {
	ix := testIndex(t)
	list, err := ix.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v, %d", err, len(list))
	}
	m := list[0]
	if m.DisplayName() != "Fixture session" {
		t.Fatalf("display name: %q", m.DisplayName())
	}
	if m.TotalTokens != 33 || m.TotalCost < 0.0109 || m.TotalCost > 0.0111 {
		t.Fatalf("totals: %d %f", m.TotalTokens, m.TotalCost)
	}
	if m.Model != "anthropic/claude-sonnet-4-5" {
		t.Fatalf("model: %q", m.Model)
	}
	if m.Mtime.Before(time.Now().Add(-time.Minute)) {
		t.Fatalf("mtime: %v", m.Mtime)
	}
	// Second call must come from cache (same result, no error).
	if again, err := ix.List(); err != nil || len(again) != 1 {
		t.Fatalf("cached List: %v", err)
	}
}

func TestMetaByKey(t *testing.T) {
	ix := testIndex(t)
	m, ok := ix.Meta("20261010_fixture")
	if !ok || m.Cwd != "/tmp/proj" {
		t.Fatalf("Meta: %v %v", ok, m)
	}
	if _, ok := ix.Meta("missing"); ok {
		t.Fatal("missing key must return false")
	}
}

func TestTranscriptActiveBranchOnly(t *testing.T) {
	ix := testIndex(t)
	meta, items, err := ix.Transcript("20261010_fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Key != "20261010_fixture" {
		t.Fatal("meta key")
	}
	var texts []string
	for _, it := range items {
		texts = append(texts, it.Kind+":"+it.Text)
	}
	joined := strings.Join(texts, "|")
	// Active branch is e1->e2->e6->e7. e2 is the common ancestor, so
	// "Hello" belongs; e3/e4 ("Hi!", toolCall, toolResult) are abandoned.
	for _, absent := range []string{"Hi!", "tool_call:read", "tool_result:read"} {
		if strings.Contains(joined, absent) {
			t.Fatalf("abandoned-branch item leaked: %s", joined)
		}
	}
	want := []string{"user:Hello", "text:Branch reply"}
	for _, w := range want {
		if !strings.Contains(joined, w) {
			t.Fatalf("missing %q in %s", w, joined)
		}
	}
	if items[0].Kind != "user" || items[0].Text != "Hello" {
		t.Fatalf("first item: %+v", items[0])
	}
	if items[1].Images != 1 || !strings.Contains(items[1].Text, "Branch msg") {
		t.Fatalf("image chip missing: %+v", items[1])
	}
}

func TestTranscriptLimit(t *testing.T) {
	ix := testIndex(t)
	_, items, err := ix.Transcript("20261010_fixture", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "text" {
		t.Fatalf("limit tail: %+v", items)
	}
}

func TestIndexConcurrent(t *testing.T) {
	// Regression: SSE streams and page handlers share one Index. Run with
	// -race; a data race here crashed the server under live streams.
	ix := testIndex(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_, _ = ix.List()
				_, _ = ix.Meta("20261010_fixture")
			}
		}()
	}
	wg.Wait()
}

func TestTranscriptMissing(t *testing.T) {
	ix := testIndex(t)
	if _, _, err := ix.Transcript("nope", 0); err == nil {
		t.Fatal("missing session must error")
	}
}

func TestTranscriptBefore(t *testing.T) {
	ix := testIndex(t)
	_, items, err := ix.Transcript("20261010_fixture", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) < 3 {
		t.Fatalf("fixture too small: %d", len(items))
	}
	last := int(items[len(items)-1].Seq)

	page, more, err := ix.TranscriptBefore("20261010_fixture", last, 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 1 || int(page[0].Seq) >= last {
		t.Fatalf("page: %+v more=%v", page, more)
	}
	if !more {
		t.Fatal("want more=true with older items left")
	}

	// before the oldest seq: empty page, no more
	first := int(items[0].Seq)
	page, more, err = ix.TranscriptBefore("20261010_fixture", first, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 0 || more {
		t.Fatalf("oldest page: %+v more=%v", page, more)
	}

	// a page of the whole branch clipped to the last two, ascending
	page, more, err = ix.TranscriptBefore("20261010_fixture", 1<<30, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 2 || int(page[0].Seq) >= int(page[1].Seq) {
		t.Fatalf("tail page: %+v", page)
	}

	if _, _, err := ix.TranscriptBefore("nope", 1<<30, 2); err == nil {
		t.Fatal("unknown key must error")
	}
}

// asstToolLine chains an assistant message carrying one tool call.
func asstToolLine(id, parent string) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	return `{"type":"message","id":"` + id + `","parentId":` + p + `,"timestamp":"2026-10-10T12:00:03.000Z","message":{"role":"assistant","content":[{"type":"toolCall","id":"call_` + id + `","name":"read","arguments":{"path":"/tmp/x"}}],"timestamp":1760000003000}}`
}

// A history page counts conversation, not raw items: the page must
// span the tool plumbing between its user/assistant text items.
func TestTranscriptBeforeWeightsConversation(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine,
		msgLine("u1", "", "one"),
		asstToolLine("t1", "u1"),
		toolResultLine("r1", "t1", 10),
		asstToolLine("t2", "r1"),
		toolResultLine("r2", "t2", 10),
		msgLine("u2", "r2", "two"),
	)
	ix := NewIndex(dir)
	_, items, err := ix.Transcript("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 6 {
		t.Fatalf("fixture items=%d", len(items))
	}

	page, _, err := ix.TranscriptBefore("s1", 1<<30, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != 6 {
		t.Fatalf("page len=%d, want the full sandwich of 6", len(page))
	}
	if page[0].Kind != "user" || page[len(page)-1].Kind != "user" {
		t.Fatalf("page ends: %s … %s", page[0].Kind, page[len(page)-1].Kind)
	}
}

// A conversation-starved stretch cannot satisfy the target, so the
// item cap bounds the page and more stays true.
func TestTranscriptBeforeCapsItemStarvedPages(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	blocks := ""
	for range transcriptPageCap + 20 {
		blocks += `{"type":"thinking","thinking":"ponder"},`
	}
	blocks = strings.TrimSuffix(blocks, ",")
	writeSession(t, path, headerLine, msgLine("u0", "", "start"),
		`{"type":"message","id":"a1","parentId":"u0","timestamp":"2026-10-10T12:00:04.000Z","message":{"role":"assistant","content":[`+blocks+`],"timestamp":1760000004000}}`)

	ix := NewIndex(dir)
	page, more, err := ix.TranscriptBefore("s1", 1<<30, 5)
	if err != nil {
		t.Fatal(err)
	}
	if len(page) != transcriptPageCap {
		t.Fatalf("page len=%d, want the cap %d", len(page), transcriptPageCap)
	}
	if !more {
		t.Fatal("want more=true: the oldest items are still above")
	}
}

// Concurrent meta misses share one parse: eight cold readers on the
// same session must produce a single full parse.
func TestCachedMetaMergesConcurrentMisses(t *testing.T) {
	ix := testIndex(t)
	list, err := ix.List()
	if err != nil || len(list) != 1 {
		t.Fatalf("List: %v, %d", err, len(list))
	}
	path := list[0].Path

	// Cold the cache and slow every parse so all eight readers land
	// inside the parse window.
	ix.mu.Lock()
	ix.cache = map[string]metaCache{}
	ix.mu.Unlock()
	var parses atomic.Int32
	prev := parseProbe
	parseProbe = func() {
		parses.Add(1)
		time.Sleep(80 * time.Millisecond)
	}
	t.Cleanup(func() { parseProbe = prev })

	var wg sync.WaitGroup
	start := make(chan struct{})
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			if _, ok := ix.cachedMeta(path); !ok {
				t.Error("cachedMeta failed")
			}
		}()
	}
	close(start)
	wg.Wait()

	if n := parses.Load(); n != 1 {
		t.Fatalf("parse ran %d times for 8 concurrent misses, want 1", n)
	}
}
