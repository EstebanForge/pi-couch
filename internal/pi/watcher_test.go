package pi

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestWatcherTicksOnSessionWrite(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "--tmp-proj--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := StartWatcher(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	if err := os.WriteFile(filepath.Join(dir, "s.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Events():
	case <-time.After(3 * time.Second):
		t.Fatal("no tick on session write")
	}
}

func TestWatcherTickOnNewWorkdirFile(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	w, err := StartWatcher(ctx, root)
	if err != nil {
		t.Fatal(err)
	}

	// New workdir created after the watcher started, then a session file.
	dir := filepath.Join(root, "--tmp-late--")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "late.jsonl"), []byte("{}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	select {
	case <-w.Events():
	case <-time.After(3 * time.Second):
		t.Fatal("no tick for late workdir file")
	}
}

func TestWatcherStop(t *testing.T) {
	root := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	w, err := StartWatcher(ctx, root)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-w.Events():
		t.Fatal("no tick expected after stop")
	case <-time.After(200 * time.Millisecond):
	}
}
