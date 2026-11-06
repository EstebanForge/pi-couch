package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// revalDir builds a sessions root with one session under a subdirectory
// (Index scans subdirs only) and returns the root, the file path.
func revalDir(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	return dir, filepath.Join(sub, "s1.jsonl")
}

// Alternating transcript streams must retain incremental tails for the four
// most recent sessions. Reading a fifth evicts the oldest instead of letting
// cached entries grow without a bound.
func TestTailCacheRetainsFourAlternatingSessions(t *testing.T) {
	root, firstPath := revalDir(t)
	paths := []string{firstPath}
	for i := 1; i < 5; i++ {
		paths = append(paths, filepath.Join(filepath.Dir(firstPath), fmt.Sprintf("s%d.jsonl", i)))
	}
	for i, path := range paths {
		writeSession(t, path, headerLine, msgLine("a", "", fmt.Sprintf("session-%d", i)))
	}

	ix := NewIndex(root)
	read := func(i int) []Entry {
		t.Helper()
		st, err := os.Stat(paths[i])
		if err != nil {
			t.Fatal(err)
		}
		entries, err := ix.tailEntries(fmt.Sprintf("s%d", i), paths[i], st)
		if err != nil {
			t.Fatal(err)
		}
		if len(entries) == 0 {
			t.Fatalf("session %d parsed no entries", i)
		}
		return entries
	}

	first := make([][]Entry, len(paths))
	for i := range paths {
		first[i] = read(i)
	}
	for i := 1; i < len(paths); i++ {
		again := read(i)
		if &again[0] != &first[i][0] {
			t.Fatalf("session %d lost its parsed tail while alternating readers", i)
		}
	}
	oldest := read(0)
	if &oldest[0] == &first[0][0] {
		t.Fatal("the fifth distinct session did not evict the oldest tail")
	}
}

// TestTailCacheDetectsSameSizeRewrite pins the cheap tail-byte check from
// the herdr-web-ui study: an in-place rewrite that keeps the file's byte
// length must not serve the cached tail. mtime and size are identical, so
// only the bytes themselves prove the change.
func TestTailCacheDetectsSameSizeRewrite(t *testing.T) {
	root, path := revalDir(t)
	writeSession(t, path, headerLine, msgLine("a", "", "alpha"))

	ix := NewIndex(root)
	if _, items, err := ix.Transcript("s1", 100); err != nil {
		t.Fatal(err)
	} else if len(items) != 1 || items[0].Text != "alpha" {
		t.Fatalf("initial items=%+v", items)
	}

	writeSession(t, path, headerLine, msgLine("a", "", "bravo"))

	_, items, err := ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Text != "bravo" {
		t.Fatalf("after same-size rewrite items=%+v, want bravo", items)
	}
}

// TestTailCacheDetectsPrefixRewriteWithAppend pins the delta-path window
// check: bytes changed ABOVE the cached offset must invalidate the tail
// even though the file also grew.
func TestTailCacheDetectsPrefixRewriteWithAppend(t *testing.T) {
	root, path := revalDir(t)
	writeSession(t, path, headerLine, msgLine("a", "", "alpha"))

	ix := NewIndex(root)
	if _, items, err := ix.Transcript("s1", 100); err != nil {
		t.Fatal(err)
	} else if len(items) != 1 || items[0].Text != "alpha" {
		t.Fatalf("initial items=%+v", items)
	}

	writeSession(t, path, headerLine,
		msgLine("a", "", "bravo"),
		msgLine("b", "a", "child"),
	)

	_, items, err := ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 || items[0].Text != "bravo" {
		t.Fatalf("after prefix rewrite items=%+v, want bravo,child", items)
	}
}

// TestTailCacheReplacedFile pins the inode check: a same-path replacement
// (fresh inode, restored mtime, same size) must not serve the old tail.
func TestTailCacheReplacedFile(t *testing.T) {
	root, path := revalDir(t)
	writeSession(t, path, headerLine, msgLine("a", "", "alpha"))

	ix := NewIndex(root)
	if _, items, err := ix.Transcript("s1", 100); err != nil {
		t.Fatal(err)
	} else if len(items) != 1 || items[0].Text != "alpha" {
		t.Fatalf("initial items=%+v", items)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	tmp := path + ".new"
	writeSession(t, tmp, headerLine, msgLine("a", "", "bravo"))
	if err := os.Chtimes(tmp, st.ModTime(), st.ModTime()); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}

	_, items, err := ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Text != "bravo" {
		t.Fatalf("after replacement items=%+v, want bravo", items)
	}
}

// TestMetaCacheRevalidatesIdentity pins the meta side: a same-size,
// same-mtime replacement must refresh SessionMeta (FirstUser here).
func TestMetaCacheRevalidatesIdentity(t *testing.T) {
	root, path := revalDir(t)
	writeSession(t, path, headerLine, msgLine("a", "", "alpha"))

	ix := NewIndex(root)
	if m, ok := ix.Meta("s1"); !ok || !strings.Contains(m.FirstUser, "alpha") {
		t.Fatalf("initial meta ok=%v firstUser=%q", ok, m.FirstUser)
	}

	st, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	tmp := path + ".new"
	writeSession(t, tmp, headerLine, msgLine("a", "", "bravo"))
	mt := st.ModTime()
	if err := os.Chtimes(tmp, mt, mt); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, path); err != nil {
		t.Fatal(err)
	}
	// mtime granularity: force-equal is the point, so pin it exactly.
	if fi, err := os.Stat(path); err != nil || !fi.ModTime().Equal(mt) {
		t.Fatalf("mtime restore failed: %v %v", err, fi)
	}

	m, ok := ix.Meta("s1")
	if !ok || !strings.Contains(m.FirstUser, "bravo") {
		t.Fatalf("after replacement meta ok=%v firstUser=%q, want bravo", ok, m.FirstUser)
	}
}
