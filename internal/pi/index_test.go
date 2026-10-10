package pi

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
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
