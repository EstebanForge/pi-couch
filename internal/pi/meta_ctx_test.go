package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// asstUsageLine is an assistant message entry carrying a usage record,
// the real jsonl shape pi writes at the end of every turn.
func asstUsageLine(id, parent string, total int64) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	usage := fmt.Sprintf(`{"input":10,"output":5,"totalTokens":%d,"cost":{"total":0.01}}`, total)
	return `{"type":"message","id":"` + id + `","parentId":` + p + `,"timestamp":"2026-10-10T12:00:03.000Z","message":{"role":"assistant","content":[{"type":"text","text":"ok"}],"usage":` + usage + `}}`
}

// CtxTokens mirrors the newest turn's usage so the client can render a
// context-window fill; earlier turns must not win.
func TestMetaCtxTokensLatest(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine,
		asstUsageLine("a1", "", 300),
		asstUsageLine("a2", "a1", 250))

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("meta not found")
	}
	if meta.CtxTokens != 250 {
		t.Fatalf("ctx tokens = %d, want 250 (latest usage wins)", meta.CtxTokens)
	}
	if meta.TotalTokens != 550 {
		t.Fatalf("total tokens = %d, want 550 (sum unchanged)", meta.TotalTokens)
	}
}

func TestMetaCtxTokensAbsent(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine, msgLine("a", "", "first"))

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("meta not found")
	}
	if meta.CtxTokens != 0 {
		t.Fatalf("ctx tokens = %d, want 0 with no usage", meta.CtxTokens)
	}
}
