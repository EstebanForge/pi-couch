package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestDirsJailsToHome(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "proj", "sub"), 0o755); err != nil {
		t.Fatal(err)
	}
	outside := t.TempDir()

	// inside home: listed with parent
	d := listDirs(home, filepath.Join(home, "proj"))
	if d.Error != "" {
		t.Fatalf("error %q", d.Error)
	}
	if d.Parent != home {
		t.Errorf("parent %q, want home", d.Parent)
	}
	found := false
	for _, e := range d.Dirs {
		if e.Name == "sub" {
			found = true
		}
	}
	if !found {
		t.Errorf("sub not listed: %+v", d.Dirs)
	}

	// escape attempt: clamped to home, never an error leak
	for _, attempt := range []string{"/etc", filepath.Join(home, "..", ".."), outside} {
		d := listDirs(home, attempt)
		if d.Error != "" {
			t.Errorf("attempt %q: error %q", attempt, d.Error)
		}
		if d.Path != home {
			t.Errorf("attempt %q: path %q, want clamped to home", attempt, d.Path)
		}
	}
}

func TestDirsSkipsSymlinksOutOfHome(t *testing.T) {
	home := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "real"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(home, "jailbreak")); err != nil {
		t.Fatal(err)
	}
	d := listDirs(home, home)
	var names []string
	for _, e := range d.Dirs {
		names = append(names, e.Name)
	}
	for _, n := range names {
		if n == "jailbreak" {
			t.Errorf("symlink out of home listed: %v", names)
		}
	}
	if len(names) != 1 || names[0] != "real" {
		t.Errorf("names %v", names)
	}
}

func TestDirsCapsAt500(t *testing.T) {
	home := t.TempDir()
	dir := filepath.Join(home, "many")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 600; i++ {
		name := filepath.Join(dir, string(rune('a'+i%26))+string(rune('a'+i/26%26))+string(rune('0'+i/676%10))+string(rune('0'+i/6760%10)))
		if err := os.MkdirAll(name, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	d := listDirs(home, dir)
	if len(d.Dirs) != 500 {
		t.Errorf("len %d, want 500", len(d.Dirs))
	}
	if !d.Truncated {
		t.Error("truncated flag not set")
	}
}

func TestAPIDirsServesJSON(t *testing.T) {
	a := &app{logger: testLogger()}
	req := httptest.NewRequest(http.MethodGet, "/api/dirs", nil)
	req.Host = "127.0.0.1:8090"
	w := httptest.NewRecorder()
	a.apiDirs(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d", w.Code)
	}
	var out dirsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Path == "" {
		t.Error("empty path")
	}
}
