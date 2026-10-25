package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

func TestMetaAutoNameWindowWins(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine,
		sessNameLine("n1", "", "Create Docs Map To Drive Full Adversarial Review Of Baluard"),
		`{"type":"custom","customType":"pi-auto-name","data":{"sessionName":"Create Docs Map To Drive Full Adversarial Review Of Baluard","windowName":"Baluard Review Map"},"id":"a1","parentId":"n1","timestamp":"2026-10-10T12:00:06.000Z"}`)

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("meta not found")
	}
	if meta.Name != "Baluard Review Map" {
		t.Fatalf("name = %q, want the auto-namer's short window name", meta.Name)
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

// The newest user entry's timestamp is the FIFO receipt the phone uses
// to retire queued-bubble pending state across reset windows.
func userAtLine(id, parent, ts string) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	return `{"type":"message","id":"` + id + `","parentId":` + p + `,"timestamp":"` + ts + `","message":{"role":"user","content":"hi","timestamp":1760000001000}}`
}

func TestMetaLastUserAt(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine,
		userAtLine("a", "", "2026-10-10T12:00:01.000Z"),
		userAtLine("b", "a", "2026-10-10T12:00:05.000Z"))

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("meta not found")
	}
	want := time.Date(2026, 10, 10, 12, 0, 5, 0, time.UTC).UnixMilli()
	if meta.LastUserAt != want {
		t.Fatalf("LastUserAt = %d, want %d", meta.LastUserAt, want)
	}
}
