package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func writeSkill(t *testing.T, dir, name, front, body string) {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(d, "SKILL.md"), []byte("---\n"+front+"---\n"+body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSkillsEndpoint(t *testing.T) {
	home := t.TempDir()
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "commit",
		"name: commit\ndescription: Create git commits with care.\n", "# commit\n")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "nodefault",
		"name: nodefault\n", "# no description, pi never loads it\n")
	writeSkill(t, filepath.Join(home, ".agents", "skills"), "dirname",
		"description: Name falls back to the directory.\n", "# x\n")
	writeSkill(t, filepath.Join(home, ".pi", "agent", "skills"), "piagent",
		"name: piagent\ndescription: From the pi agent skills dir.\n", "# y\n")

	a, _, h := streamTestApp(t)
	a.home = home

	req := httptest.NewRequest(http.MethodGet, "/api/skills", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Skills []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	names := make([]string, 0, len(out.Skills))
	for _, s := range out.Skills {
		names = append(names, s.Name)
	}
	want := []string{"commit", "dirname", "piagent"}
	if len(names) != len(want) {
		t.Fatalf("skills %v, want %v", names, want)
	}
	for i := range want {
		if names[i] != want[i] {
			t.Fatalf("skills %v, want %v", names, want)
		}
	}
	if out.Skills[1].Description != "Name falls back to the directory." {
		t.Fatalf("dirname description %q", out.Skills[1].Description)
	}
}

func TestSkillsProjectDirs(t *testing.T) {
	home := t.TempDir()
	cwd := filepath.Join(home, "code", "repo", "pkg")
	if err := os.MkdirAll(filepath.Join(home, "code", "repo", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(home, "code", ".agents", "skills", "shared"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, "code", ".agents", "skills", "shared", "SKILL.md"),
		[]byte("---\nname: shared\ndescription: Above the repo root, out of scope.\n---\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeSkill(t, filepath.Join(home, "code", "repo", ".agents", "skills"), "reposkill",
		"name: reposkill\ndescription: Project skill at the repo root.\n", "# z\n")

	a, _, h := streamTestApp(t)
	a.home = home

	req := httptest.NewRequest(http.MethodGet, "/api/skills?cwd="+cwd, nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	var out struct {
		Skills []struct {
			Name string `json:"name"`
		} `json:"skills"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Skills) != 1 || out.Skills[0].Name != "reposkill" {
		t.Fatalf("skills %+v, want only reposkill", out.Skills)
	}
}

func TestSkillsSymlinkedDirs(t *testing.T) {
	home := t.TempDir()
	// pi's own set keeps skill dirs as symlinks into a central repo;
	// the scan must follow them or the phone sees one real dir only.
	real := filepath.Join(home, "elsewhere", "commit")
	writeSkill(t, filepath.Join(home, "elsewhere"), "commit",
		"name: commit\ndescription: Create git commits with care.\n", "# c\n")
	if err := os.MkdirAll(filepath.Join(home, ".agents", "skills"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, filepath.Join(home, ".agents", "skills", "commit")); err != nil {
		t.Fatal(err)
	}
	// a symlink loop must terminate, not spin the walk
	loop := filepath.Join(home, ".agents", "skills", "loop")
	if err := os.Symlink(filepath.Join(home, ".agents", "skills"), loop); err != nil {
		t.Fatal(err)
	}

	a, _, _ := streamTestApp(t)
	a.home = home
	skills := scanPiSkills(a.home, "")
	if len(skills) != 1 || skills[0].Name != "commit" {
		t.Fatalf("skills %+v, want commit", skills)
	}
}

func TestPromptsSymlinkedFiles(t *testing.T) {
	home := t.TempDir()
	// this box's prompt files are symlinks into a central repo
	real := filepath.Join(home, "elsewhere")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(real, "review.md"), []byte(
		"---\ndescription: Review staged git changes\nargument-hint: \"[focus]\"\n---\nReview things.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	prompts := filepath.Join(home, ".pi", "agent", "prompts")
	if err := os.MkdirAll(prompts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(real, "review.md"), filepath.Join(prompts, "review.md")); err != nil {
		t.Fatal(err)
	}

	got := scanPiPrompts(home, "")
	if len(got) != 1 || got[0].Name != "review" || got[0].Hint != "[focus]" {
		t.Fatalf("prompts %+v, want review with its hint", got)
	}
}

func TestPromptsEndpoint(t *testing.T) {
	home := t.TempDir()
	prompts := filepath.Join(home, ".pi", "agent", "prompts")
	if err := os.MkdirAll(prompts, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prompts, "review.md"), []byte(
		"---\ndescription: Review staged git changes\nargument-hint: \"[focus]\"\n---\nReview things.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prompts, "nodef.md"), []byte(
		"First line becomes the description.\n\nMore body.\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prompts, "ignored.txt"), []byte("not markdown\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	a, _, h := streamTestApp(t)
	a.home = home

	req := httptest.NewRequest(http.MethodGet, "/api/prompts", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Prompts []struct {
			Name        string `json:"name"`
			Description string `json:"description"`
			Hint        string `json:"hint"`
		} `json:"prompts"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if len(out.Prompts) != 2 {
		t.Fatalf("prompts %+v, want 2", out.Prompts)
	}
	if out.Prompts[0].Name != "nodef" || out.Prompts[0].Description != "First line becomes the description." {
		t.Fatalf("nodef %+v", out.Prompts[0])
	}
	if out.Prompts[1].Name != "review" || out.Prompts[1].Description != "Review staged git changes" || out.Prompts[1].Hint != "[focus]" {
		t.Fatalf("review %+v", out.Prompts[1])
	}
}
