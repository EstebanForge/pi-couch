package pi

import (
	"os"
	"path/filepath"
	"testing"
)

func writeSession(t *testing.T, path string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(joinLines(lines)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func joinLines(lines []string) string {
	out := ""
	for _, l := range lines {
		out += l + "\n"
	}
	return out
}

const headerLine = `{"type":"session","version":3,"id":"11111111-2222-3333-4444-555555555555","timestamp":"2026-10-10T12:00:00.000Z","cwd":"/tmp/proj"}`

// msgLine chains to parent so ActivePath keeps every entry in view.
func msgLine(id, parent, text string) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	return `{"type":"message","id":"` + id + `","parentId":` + p + `,"timestamp":"2026-10-10T12:00:01.000Z","message":{"role":"user","content":"` + text + `","timestamp":1760000001000}}`
}

func TestTranscriptIncrementalAppend(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine, msgLine("a", "", "first"))

	ix := NewIndex(dir)
	_, items, err := ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("initial items=%d", len(items))
	}

	// Append two lines: only the delta must be parsed, and the new
	// content must appear exactly once.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(msgLine("b", "a", "second") + "\n" + msgLine("c", "b", "third") + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, items, err = ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("after append items=%d, want 3", len(items))
	}

	// Repeated calls without growth must stay stable (cache hit).
	_, items, err = ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 3 {
		t.Fatalf("stable call items=%d", len(items))
	}
}

func TestTranscriptTornTrailingLine(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine, msgLine("a", "", "first"))

	ix := NewIndex(dir)
	if _, _, err := ix.Transcript("s1", 100); err != nil {
		t.Fatal(err)
	}

	// A partial line lands (the writer is mid-write). It must NOT
	// appear yet, and must appear exactly once once completed.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	half := msgLine("b", "a", "second")[:len(msgLine("b", "a", "second"))/2]
	f.WriteString(half)
	f.Close()

	_, items, err := ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("torn line leaked: items=%d", len(items))
	}

	f, err = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	f.WriteString(msgLine("b", "a", "second")[len(msgLine("b", "a", "second"))/2:] + "\n")
	f.Close()

	_, items, err = ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("completed line items=%d, want 2 (no duplicate)", len(items))
	}
}

func TestTranscriptTruncatedRerunsFull(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine, msgLine("a", "", "one"), msgLine("b", "a", "two"))

	ix := NewIndex(dir)
	if _, items, err := ix.Transcript("s1", 100); err != nil {
		t.Fatal(err)
	} else if len(items) != 2 {
		t.Fatalf("initial items=%d", len(items))
	}

	// The file shrinks (rotation, rewrite): the cache must not serve
	// stale entries or mis-parse.
	writeSession(t, path, headerLine, msgLine("z", "", "fresh"))
	_, items, err := ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Text != "fresh" {
		t.Fatalf("after truncate items=%d first=%+v", len(items), items[0])
	}
}
