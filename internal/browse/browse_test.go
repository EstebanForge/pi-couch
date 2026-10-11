package browse

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	write := func(rel, content string) {
		if err := os.MkdirAll(filepath.Dir(filepath.Join(root, rel)), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(root, rel), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	write("cmd/main.go", "package main\n")
	write("README.md", "# hello\n")
	write("bin.dat", "\x00\x01binary")
	if err := os.MkdirAll(filepath.Join(root, "empty"), 0o755); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestList(t *testing.T) {
	root := tree(t)
	entries, err := List(root, "")
	if err != nil {
		t.Fatal(err)
	}
	// Dirs first, then files, each alphabetical.
	var dirs, files []string
	for _, e := range entries {
		if e.Dir {
			dirs = append(dirs, e.Name)
		} else {
			files = append(files, e.Name)
		}
	}
	if strings.Join(dirs, ",") != "cmd,empty" || strings.Join(files, ",") != "bin.dat,README.md" {
		t.Fatalf("order: dirs=%v files=%v", dirs, files)
	}
	sub, err := List(root, "cmd")
	if err != nil || len(sub) != 1 || sub[0].Name != "main.go" {
		t.Fatalf("sub: %v %v", err, sub)
	}
}

func TestPreview(t *testing.T) {
	root := tree(t)
	if text, kind, _ := Preview(root, "README.md"); kind != "text" || text != "# hello\n" {
		t.Fatalf("text preview: %q %q", kind, text)
	}
	if _, kind, _ := Preview(root, "bin.dat"); kind != "binary" {
		t.Fatalf("binary: %q", kind)
	}
}

func TestPreviewLimits(t *testing.T) {
	root := t.TempDir()
	big := strings.Repeat("x", PreviewLimit+10)
	if err := os.WriteFile(filepath.Join(root, "big.txt"), []byte(big), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, kind, _ := Preview(root, "big.txt"); kind != "toolarge" {
		t.Fatalf("toolarge: %q", kind)
	}
	med := strings.Repeat("y", MaxPreviewRender+10)
	if err := os.WriteFile(filepath.Join(root, "med.txt"), []byte(med), 0o644); err != nil {
		t.Fatal(err)
	}
	if text, kind, _ := Preview(root, "med.txt"); kind != "partial" || len(text) != MaxPreviewRender {
		t.Fatalf("partial: %q %d", kind, len(text))
	}
}

func TestEscape(t *testing.T) {
	root := tree(t)
	for _, rel := range []string{"../outside", "/etc"} {
		if _, err := List(root, rel); err == nil {
			t.Fatalf("List accepted %q", rel)
		}
	}
	// Dotdots that clean back inside the root are fine.
	if _, err := List(root, "cmd/../../."); err != nil {
		t.Fatalf("root via dotdots: %v", err)
	}
	if _, _, err := Preview(root, "cmd/../README.md"); err != nil {
		t.Fatalf("within-root dotdot should resolve: %v", err)
	}
	// Symlink pointing out of the root must be refused.
	outside := t.TempDir()
	if err := os.Symlink(outside, filepath.Join(root, "out")); err != nil {
		t.Fatal(err)
	}
	if _, err := List(root, "out"); err == nil {
		t.Fatal("symlinked dir accepted")
	}
	if _, _, err := Preview(root, "out/whatever"); err != ErrEscape {
		t.Fatalf("symlink escape: %v", err)
	}
}
