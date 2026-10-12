package pi

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestParseEntriesOversizeLine(t *testing.T) {
	// One giant entry (image base64) must not truncate the entries after it:
	// bufio.Scanner dies on ErrTooLong and silently drops the rest of the file.
	big := `{"type":"custom_message","customType":"x","id":"big","summary":"` +
		strings.Repeat("a", 12*1024*1024) + `"}`
	content := "{\"type\":\"session\",\"id\":\"s1\"}\n" + big +
		"\n{\"type\":\"message\",\"id\":\"m1\",\"message\":{\"role\":\"user\",\"content\":\"hello\"}}\n"
	entries := ParseEntries(strings.NewReader(content))
	// The header line also decodes as an entry, hence 3.
	if len(entries) != 3 {
		t.Fatalf("oversize line truncated the file: got %d entries, want 3", len(entries))
	}
	if entries[2].ID != "m1" {
		t.Fatalf("tail entry lost: %+v", entries[2])
	}
}

func TestActivePathCycleTerminates(t *testing.T) {
	// Malformed tree: A and B are each other's parent. Must not hang.
	pa, pb := "b", "a"
	entries := []Entry{
		{ID: "a", ParentID: &pa},
		{ID: "b", ParentID: &pb},
	}
	done := make(chan []Entry, 1)
	go func() { done <- ActivePath(entries) }()
	select {
	case path := <-done:
		_ = path // nil or whatever, just no hang
	case <-time.After(2 * time.Second):
		t.Fatal("ActivePath looped forever on a parent cycle")
	}
}

func TestTruncateMultibyte(t *testing.T) {
	s := strings.Repeat("é", 100) // 200 bytes, 100 runes
	got := truncate(s, 10)
	if strings.HasSuffix(got, "\u2026") && len(got) > 40 {
		t.Fatalf("truncate cut mid-rune: %d bytes for %q", len(got), got)
	}
	if !strings.HasSuffix(got, "…") {
		t.Fatalf("want ellipsis, got %q", got)
	}
}

func TestMetaRejectsTraversal(t *testing.T) {
	ix := NewIndex(t.TempDir())
	for _, key := range []string{"", "..", "a/b", `a\b`, "../../etc/passwd"} {
		if _, ok := ix.Meta(key); ok {
			t.Errorf("Meta(%q) accepted", key)
		}
	}
}

func TestWatcherFansOut(t *testing.T) {
	root := t.TempDir()
	wd := filepath.Join(root, "dev1")
	if err := os.MkdirAll(filepath.Join(wd, "x"), 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := StartWatcher(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	s1, unsub1 := w.Subscribe()
	s2, unsub2 := w.Subscribe()
	defer unsub1()
	defer unsub2()

	time.Sleep(100 * time.Millisecond) // fsnotify settle
	if err := os.WriteFile(filepath.Join(wd, "s.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, ch := range map[string]<-chan struct{}{"s1": s1, "s2": s2} {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
			t.Fatalf("%s never ticked", name)
		}
	}
	// A cancelled subscription no longer ticks.
	unsub1()
	if err := os.WriteFile(filepath.Join(wd, "s.jsonl"), []byte("{}\n{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-s1:
		t.Fatal("unsubscribed channel still ticked")
	case <-time.After(500 * time.Millisecond):
	}
	select {
	case <-s2:
	case <-time.After(3 * time.Second):
		t.Fatal("s2 lost ticks after s1 unsubscribed")
	}
}
