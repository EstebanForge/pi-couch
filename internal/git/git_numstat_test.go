package git

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

func gitRun(t *testing.T, dir string, args ...string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, out)
	}
}

func TestStatusRenameCounts(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", ".")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "olt.txt"), []byte("uno\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "init")
	// Staged rename plus a worktree edit on the new name.
	gitRun(t, dir, "mv", "olt.txt", "nuevaño.txt")
	if err := os.WriteFile(filepath.Join(dir, "nuevaño.txt"), []byte("uno\ndos\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	files, err := Status(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	var f *FileChange
	for i := range files {
		if files[i].Path == "nuevaño.txt" {
			f = &files[i]
		}
	}
	if f == nil {
		t.Fatalf("renamed file missing from status: %+v", files)
	}
	if f.Label() != "ren" {
		t.Errorf("label: got %q, want ren", f.Label())
	}
	if f.Added != 1 || f.Deleted != 0 {
		// The unstaged numstat reports 1/0 for the new path; the staged
		// rename record carries old+new paths and must not clobber it.
		t.Errorf("numstat counts lost for non-ascii rename: got +%d/-%d", f.Added, f.Deleted)
	}
}

func TestStatusCopyConsumesOldPath(t *testing.T) {
	dir := t.TempDir()
	gitRun(t, dir, "init", "-q", ".")
	gitRun(t, dir, "config", "user.email", "t@t")
	gitRun(t, dir, "config", "user.name", "t")
	if err := os.WriteFile(filepath.Join(dir, "orig.txt"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", ".")
	gitRun(t, dir, "commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(dir, "copia.txt"), []byte("data\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gitRun(t, dir, "add", "-A")
	gitRun(t, dir, "-c", "diff.detectRenames=true", "status") // warm up
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	files, err := Status(ctx, dir)
	if err != nil {
		t.Fatal(err)
	}
	// Whatever git reports (C or A for copia.txt), every entry must carry a
	// real path, not the old path or an empty string.
	for _, f := range files {
		if f.Path == "" || f.Path == "orig.txt" && f.Staged == "C" {
			t.Errorf("entry path not the new path: %+v", f)
		}
	}
}
