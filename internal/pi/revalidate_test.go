package pi

import (
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
