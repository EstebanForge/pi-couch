package git

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func tempRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	git("init", "-q", "-b", "main")
	write(dir, "a.txt", "one\n")
	git("add", ".")
	git("commit", "-q", "-m", "init")
	return dir
}

func write(dir, name, content string) {
	if err := os.WriteFile(filepath.Join(dir, name), []byte(content), 0o644); err != nil {
		panic(err)
	}
}

func TestToplevelAndBranch(t *testing.T) {
	repo := tempRepo(t)
	top, err := Toplevel(t.Context(), repo)
	if err != nil || filepath.Base(top) != filepath.Base(repo) {
		t.Fatalf("Toplevel: %v %q", err, top)
	}
	if b := Branch(t.Context(), repo); b != "main" {
		t.Fatalf("Branch: %q", b)
	}
	if _, err := Toplevel(t.Context(), t.TempDir()); err != ErrNotRepo {
		t.Fatalf("non-repo: %v", err)
	}
}

func TestStatus(t *testing.T) {
	repo := tempRepo(t)
	write(repo, "a.txt", "one\ntwo\nthree\n") // worktree modify
	write(repo, "new.txt", "hello\n")         // untracked
	if err := exec.Command("git", "-C", repo, "add", "a.txt").Run(); err != nil {
		t.Fatal(err)
	}
	files, err := Status(t.Context(), repo)
	if err != nil {
		t.Fatal(err)
	}
	byPath := map[string]FileChange{}
	for _, f := range files {
		byPath[f.Path] = f
	}
	a := byPath["a.txt"]
	if a.Label() != "mod" || a.Added != 2 || a.Deleted != 0 {
		t.Fatalf("a.txt: %+v", a)
	}
	if n := byPath["new.txt"]; n.Label() != "new" {
		t.Fatalf("new.txt: %+v", n)
	}
}

func TestBinaryAndRename(t *testing.T) {
	repo := tempRepo(t)
	write(repo, "bin.dat", "\x00\x01\x02binary")
	if err := exec.Command("git", "-C", repo, "add", "bin.dat").Run(); err != nil {
		t.Fatal(err)
	}
	files, _ := Status(t.Context(), repo)
	if len(files) != 1 || !files[0].Binary {
		t.Fatalf("binary status: %+v", files)
	}
	if _, _, err := Diff(t.Context(), repo, "bin.dat", true); err != nil {
		t.Fatal(err)
	}

	if err := exec.Command("git", "-C", repo, "commit", "-q", "-m", "bin").Run(); err != nil {
		t.Fatal(err)
	}
	if err := exec.Command("git", "-C", repo, "mv", "a.txt", "b.txt").Run(); err != nil {
		t.Fatal(err)
	}
	files, _ = Status(t.Context(), repo)
	if len(files) != 1 || files[0].Label() != "ren" || files[0].Path != "b.txt" {
		t.Fatalf("rename status: %+v", files)
	}
}

func TestDiffTruncation(t *testing.T) {
	repo := tempRepo(t)
	var b strings.Builder
	for i := 0; i < DiffLimit+50; i++ {
		b.WriteString("line\n")
	}
	write(repo, "big.txt", b.String())
	if err := exec.Command("git", "-C", repo, "add", "big.txt").Run(); err != nil {
		t.Fatal(err)
	}
	text, truncated, err := Diff(t.Context(), repo, "big.txt", true)
	if err != nil || !truncated {
		t.Fatalf("truncation: %v %v", truncated, err)
	}
	if !truncated {
		t.Fatal("expected truncation")
	}
	if n := len(strings.Split(text, "\n")); n != DiffLimit {
		t.Fatalf("lines: %d", n)
	}
}
