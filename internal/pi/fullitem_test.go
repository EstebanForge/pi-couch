package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// toolResultLine builds a chained toolResult message whose text is
// n 'x' characters, so truncation tests can size the payload exactly.
func toolResultLine(id, parent string, n int) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	text := strings.Repeat("x", n)
	return `{"type":"message","id":"` + id + `","parentId":` + p + `,"timestamp":"2026-10-10T12:00:02.000Z","message":{"role":"toolResult","toolName":"read","content":[{"type":"text","text":"` + text + `"}],"timestamp":1760000002000}}`
}

func TestFullItemReturnsUntruncatedText(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine, msgLine("a", "", "first"), toolResultLine("b", "a", 1000))

	ix := NewIndex(dir)
	_, items, err := ix.Transcript("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	var trunc *ViewItem
	for i := range items {
		if items[i].Kind == "tool_result" {
			trunc = &items[i]
		}
	}
	if trunc == nil {
		t.Fatal("no tool_result item in transcript")
	}
	if !trunc.Truncated {
		t.Fatalf("tool_result should be flagged truncated")
	}
	if got := trunc.Text; got != truncate(strings.Repeat("x", 1000), 400) {
		t.Fatalf("transcript text %q... not the capped render", got[:20])
	}

	meta, full, err := ix.FullItem("s1", trunc.Seq)
	if err != nil {
		t.Fatal(err)
	}
	if meta.Key != "s1" {
		t.Fatalf("meta key = %q", meta.Key)
	}
	if full.Kind != "tool_result" {
		t.Fatalf("kind = %q", full.Kind)
	}
	if full.Truncated {
		t.Fatal("full item must not be flagged truncated")
	}
	if got := full.Text; got != strings.Repeat("x", 1000) {
		t.Fatalf("full text len = %d, want 1000", len(got))
	}
}

func TestFullItemShortItemNotFlagged(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine, msgLine("a", "", "first"), toolResultLine("b", "a", 10))

	ix := NewIndex(dir)
	_, items, err := ix.Transcript("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items {
		if it.Kind == "tool_result" && it.Truncated {
			t.Fatal("short tool_result must not be flagged truncated")
		}
	}

	// A multiline tool result under the cap flattens in render (that is
	// truncate's shape) but must not read as truncated.
	ml := `"line1\nline2\nline3"`
	writeSession(t, filepath.Join(sub, "s2.jsonl"), headerLine, msgLine("a", "", "first"),
		`{"type":"message","id":"b","parentId":"a","timestamp":"2026-10-10T12:00:02.000Z","message":{"role":"toolResult","toolName":"read","content":[{"type":"text","text":[`+ml+`]},"timestamp":1760000002000}}`)
	_, items2, err := ix.Transcript("s2", 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, it := range items2 {
		if it.Truncated {
			t.Fatalf("multiline short item wrongly flagged: %+v", it)
		}
	}

	if _, _, err := ix.FullItem("s1", 1); err != nil {
		t.Fatalf("FullItem on a short item: %v", err)
	}
	if _, _, err := ix.FullItem("s1", 999); err == nil {
		t.Fatal("FullItem on an unknown seq must error")
	}
	if _, _, err := ix.FullItem("nope", 0); err == nil {
		t.Fatal("FullItem on an unknown session must error")
	}
}
