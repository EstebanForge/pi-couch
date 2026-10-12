package pi

import (
	"os"
	"path/filepath"
	"testing"
)

// Seq must give every view item a stable identity on the active branch so
// the JSON stream can key rows and tell appends from re-renders.
func TestTranscriptSeq(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path,
		headerLine,
		msgLine("a", "", "first"),
		msgLine("b", "a", "second"),
		msgLine("c", "b", "third"),
	)
	ix := NewIndex(dir)

	_, items, err := ix.Transcript("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	for i, it := range items {
		if it.Seq != i {
			t.Fatalf("item %d carries seq %d", i, it.Seq)
		}
	}

	// A cached reread must return identical seqs.
	_, again, err := ix.Transcript("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	for i := range items {
		if again[i].Seq != items[i].Seq {
			t.Fatalf("cached reread seq %d != %d", again[i].Seq, items[i].Seq)
		}
	}

	// Growth must continue the numbering, never renumber from zero.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString(msgLine("d", "c", "fourth") + "\n"); err != nil {
		t.Fatal(err)
	}
	f.Close()

	_, grown, err := ix.Transcript("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(grown) != 4 {
		t.Fatalf("after growth items=%d, want 4", len(grown))
	}
	if last := grown[3]; last.Seq != 3 {
		t.Fatalf("grown item seq=%d, want 3", last.Seq)
	}

	// A tail cut keeps original seqs: the newest item alone is seq 3.
	_, cut, err := ix.Transcript("s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if len(cut) != 1 || cut[0].Seq != 3 {
		t.Fatalf("cut items=%d seq=%d, want [3]", len(cut), cut[0].Seq)
	}
}

// The last thinking_level_change on the parsed entries wins; no entry
// means the empty string, which the wire calls unknown.
func TestMetaThinking(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s2.jsonl")
	writeSession(t, path,
		headerLine,
		msgLine("a", "", "first"),
		`{"type":"thinking_level_change","id":"t1","parentId":"a","timestamp":"2026-10-10T12:00:02.000Z","thinkingLevel":"medium"}`,
		`{"type":"thinking_level_change","id":"t2","parentId":"t1","timestamp":"2026-10-10T12:00:03.000Z","thinkingLevel":"high"}`,
	)
	ix := NewIndex(dir)
	meta, ok := ix.Meta("s2")
	if !ok {
		t.Fatal("meta missing")
	}
	if meta.Thinking != "high" {
		t.Fatalf("thinking=%q, want high", meta.Thinking)
	}

	path3 := filepath.Join(sub, "s3.jsonl")
	writeSession(t, path3, headerLine, msgLine("a", "", "first"))
	if _, ok := ix.Meta("s3"); !ok {
		t.Fatal("meta s3 missing")
	}
	meta3, _ := ix.Meta("s3")
	if meta3.Thinking != "" {
		t.Fatalf("thinking=%q, want empty", meta3.Thinking)
	}
}
