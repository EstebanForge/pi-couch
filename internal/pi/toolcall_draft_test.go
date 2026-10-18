package pi

import (
	"os"
	"path/filepath"
	"testing"
)

func TestToolCallDraft(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "2026-10-18T00-00-00-000Z_a.jsonl")
	header := `{"type":"session","version":3,"id":"11111111-2222-3333-4444-555555555555","timestamp":"2026-10-18T00:00:00.000Z","cwd":"` + sub + `"}` + "\n"
	noise := `{"type":"message","id":"n1","message":{"role":"user","content":"commit it"}}` + "\n"
	draft := `{"type":"message","id":"n2","message":{"role":"assistant","content":[{"type":"text","text":"working"},{"type":"toolCall","id":"c1","name":"git_commit","arguments":{"subject":"feat: draft","body":"line one\nline two"}}]}}` + "\n"
	after := `{"type":"message","id":"n3","message":{"role":"toolResult","toolCallId":"c1","toolName":"git_commit","content":[{"type":"text","text":"git-me: cancelled"}]}}` + "\n"
	if err := os.WriteFile(path, []byte(header+noise+draft+after), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(dir)
	got := ix.ToolCallDraft("2026-10-18T00-00-00-000Z_a")
	want := "feat: draft\n\nline one\nline two"
	if got != want {
		t.Fatalf("draft %q, want %q", got, want)
	}
}

func TestToolCallDraftNone(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "proj")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "2026-10-18T00-00-01-000Z_b.jsonl")
	if err := os.WriteFile(path, []byte(`{"type":"session","version":3,"id":"11111111-2222-3333-4444-555555555556","timestamp":"2026-10-18T00:00:01.000Z","cwd":"`+sub+`"}`+"\n"+`{"type":"message","id":"n1","message":{"role":"user","content":"hi"}}`+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ix := NewIndex(dir)
	if got := ix.ToolCallDraft("2026-10-18T00-00-01-000Z_b"); got != "" {
		t.Fatalf("no call must give empty, got %q", got)
	}
}
