package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// pi's session name lives only in session_info entries: the latest wins and
// an empty name explicitly clears it (pi's getSessionName walks in reverse).
func sessNameLine(id, parent, name string) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	n := "null"
	if name != "" {
		n = `"` + name + `"`
	}
	return `{"type":"session_info","id":"` + id + `","parentId":` + p + `,"timestamp":"2026-10-10T12:00:05.000Z","name":` + n + `}`
}

func TestMetaSessionNameLatestWins(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine,
		sessNameLine("n1", "", "First Name"),
		sessNameLine("n2", "n1", "Second Name"))

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("meta not found")
	}
	if meta.Name != "Second Name" {
		t.Fatalf("name = %q, want latest session_info name", meta.Name)
	}
}

func TestMetaSessionNameClearWins(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine,
		sessNameLine("n1", "", "First Name"),
		sessNameLine("n2", "n1", ""),
		msgLine("a", "", "hello"))

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("meta not found")
	}
	if meta.Name != "" {
		t.Fatalf("name = %q, want empty (a later empty session_info clears it)", meta.Name)
	}
	if !strings.Contains(meta.FirstUser, "hello") {
		t.Fatalf("first_user = %q, want the first user text", meta.FirstUser)
	}
}
