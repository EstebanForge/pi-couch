package server

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
	"github.com/EstebanForge/couch-pi/internal/store"
)

// makeRepo builds a tiny git repo with one committed file and one
// unstaged modification, so the changes and files endpoints have real
// ground truth to read.
func makeRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %s", args, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("line one\nline two\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", ".")
	run("commit", "-qm", "init")
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("line one\nline two changed\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return dir
}

// repoTestApp is streamTestApp with the fixture session's cwd pointed
// at the given directory, so git-backed endpoints resolve.
func repoTestApp(t *testing.T, cwd string) (http.Handler, string) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	header := fmt.Sprintf(`{"type":"session","version":3,"id":"11111111-2222-3333-4444-555555555555","timestamp":"2026-10-10T12:00:00.000Z","cwd":%q}`, cwd)
	writeLines(t, filepath.Join(sub, "s1.jsonl"), header, userLine("a", "", "first"))
	w, err := pi.StartWatcher(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	_ = w
	st, serr := store.NewDir(t.TempDir())
	if serr != nil {
		t.Fatal(serr)
	}
	a := &app{
		logger:  slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:     pi.NewIndex(root),
		herdr:   herdr.New(),
		watcher: w,
		st:      st,
	}
	// Meta caches by mtime; give the index a beat to see the file.
	h := a.guard(a.routes())
	return h, sub
}

func getJSON(t *testing.T, h http.Handler, path string) (int, map[string]any) {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:8090" // the guard host-gates /api reads
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var body map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &body)
	return rec.Code, body
}

func TestAPIChangesList(t *testing.T) {
	repo := makeRepo(t)
	h, _ := repoTestApp(t, repo)
	code, body := getJSON(t, h, "/api/sessions/s1/changes")
	if code != http.StatusOK {
		t.Fatalf("changes: %d %v", code, body)
	}
	if body["repo"] != true {
		t.Fatalf("repo flag: %v", body["repo"])
	}
	files, ok := body["files"].([]any)
	if !ok || len(files) == 0 {
		t.Fatalf("no files in %v", body)
	}
	f := files[0].(map[string]any)
	if f["path"] != "hello.txt" || f["label"] != "mod" || f["binary"] != false {
		t.Fatalf("file entry: %v", f)
	}
	if f["added"] == float64(0) && f["deleted"] == float64(0) {
		t.Fatalf("numstat counts missing: %v", f)
	}
}

func TestAPIChangesNoRepo(t *testing.T) {
	h, _ := repoTestApp(t, t.TempDir())
	code, body := getJSON(t, h, "/api/sessions/s1/changes")
	if code != http.StatusOK || body["repo"] != false {
		t.Fatalf("no-repo changes: %d %v", code, body)
	}
}

func TestAPIChangesDiff(t *testing.T) {
	repo := makeRepo(t)
	h, _ := repoTestApp(t, repo)
	code, body := getJSON(t, h, "/api/sessions/s1/changes?file=hello.txt")
	if code != http.StatusOK {
		t.Fatalf("diff: %d %v", code, body)
	}
	diff, _ := body["diff"].(string)
	if !strings.Contains(diff, "line two changed") || !strings.Contains(diff, "@@") {
		t.Fatalf("diff body: %q", diff)
	}
	if body["file"] != "hello.txt" {
		t.Fatalf("file echo: %v", body["file"])
	}
	// A path git does not report is a 404, not a traversal.
	code, _ = getJSON(t, h, "/api/sessions/s1/changes?file=../etc/passwd")
	if code != http.StatusNotFound {
		t.Fatalf("unknown file: %d, want 404", code)
	}
}

func TestAPIFilesListAndView(t *testing.T) {
	repo := makeRepo(t)
	h, _ := repoTestApp(t, repo)
	code, body := getJSON(t, h, "/api/sessions/s1/files")
	if code != http.StatusOK || body["repo"] != true {
		t.Fatalf("files: %d %v", code, body)
	}
	entries := body["entries"].([]any)
	var names []string
	for _, e := range entries {
		names = append(names, e.(map[string]any)["name"].(string))
	}
	if !strings.Contains(strings.Join(names, ","), "hello.txt") {
		t.Fatalf("entries: %v", names)
	}
	code, body = getJSON(t, h, "/api/sessions/s1/files/view?file=hello.txt")
	if code != http.StatusOK {
		t.Fatalf("view: %d %v", code, body)
	}
	if !strings.Contains(body["content"].(string), "line two changed") || body["kind"] != "text" {
		t.Fatalf("view body: %v", body)
	}
	code, _ = getJSON(t, h, "/api/sessions/s1/files/view?file=missing.txt")
	if code != http.StatusBadRequest {
		t.Fatalf("missing file: %d, want 400", code)
	}
}

func TestAPICommentsSent(t *testing.T) {
	h, _ := repoTestApp(t, t.TempDir())
	code, body := getJSON(t, h, "/api/sessions/s1/comments")
	if code != http.StatusOK {
		t.Fatalf("comments: %d %v", code, body)
	}
	if _, ok := body["sent"].([]any); !ok {
		t.Fatalf("sent array missing: %v", body)
	}
}
