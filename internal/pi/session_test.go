package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func loadFixture(t *testing.T) (Header, []Entry) {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", "fixture.jsonl"))
	if err != nil {
		t.Fatalf("open fixture: %v", err)
	}
	defer f.Close()
	h, err := ParseHeader(f)
	if err != nil {
		t.Fatalf("ParseHeader: %v", err)
	}
	if _, err := f.Seek(0, 0); err != nil {
		t.Fatal(err)
	}
	return h, ParseEntries(f)
}

func TestHeader(t *testing.T) {
	h, _ := loadFixture(t)
	if h.Version != 3 || h.ID == "" || h.Cwd != "/tmp/proj" {
		t.Fatalf("bad header: %+v", h)
	}
}

func TestParseTolerant(t *testing.T) {
	_, entries := loadFixture(t)
	// 9 parseable lines: header, e1..e7, e5b; the malformed line is dropped.
	if len(entries) != 9 {
		t.Fatalf("want 9 entries, got %d", len(entries))
	}
	kinds := map[string]int{}
	for _, e := range entries {
		kinds[e.Type]++
	}
	if kinds["future_thing"] != 1 {
		t.Fatal("unknown entry type must still decode and survive")
	}
}

func TestContentStringAndBlocks(t *testing.T) {
	_, entries := loadFixture(t)
	var user, assistant *Message
	for _, e := range entries {
		if e.Message == nil {
			continue
		}
		switch e.ID {
		case "e6":
			user = e.Message
		case "e3":
			assistant = e.Message
		}
	}
	blocks := user.Blocks()
	if len(blocks) != 2 || blocks[0].Text != "Branch msg" || blocks[1].Type != "image" {
		t.Fatalf("user blocks: %+v", blocks)
	}
	ablocks := assistant.Blocks()
	if len(ablocks) != 3 || ablocks[0].Type != "thinking" || ablocks[2].Name != "read" {
		t.Fatalf("assistant blocks: %+v", ablocks)
	}
}

func TestActivePathPicksLastLeaf(t *testing.T) {
	_, entries := loadFixture(t)
	path := ActivePath(entries)
	// Active branch: e1 -> e2 -> e6 -> e7. The main line e3/e4 is abandoned.
	if len(path) != 4 {
		t.Fatalf("want 4 entries on active path, got %d", len(path))
	}
	if path[0].ID != "e1" || path[len(path)-1].ID != "e7" {
		t.Fatalf("wrong endpoints: %s..%s", path[0].ID, path[len(path)-1].ID)
	}
	for _, e := range path {
		if e.ID == "e3" || e.ID == "e4" {
			t.Fatalf("abandoned branch leaked into active path: %s", e.ID)
		}
	}
}

func TestPartialFinalLine(t *testing.T) {
	// A torn final line (live writer) must be skipped, not fail.
	r := strings.NewReader("{\"type\":\"session\",\"version\":3,\"id\":\"x\",\"cwd\":\"/p\"}\n{\"type\":\"session_info\"")
	entries := ParseEntries(r)
	if len(entries) != 1 || entries[0].Type != "session" {
		t.Fatalf("want only the complete line, got %d entries", len(entries))
	}
}

// Integration: parse real sessions when present; skip elsewhere.
func TestRealSessionsParse(t *testing.T) {
	root := os.Getenv("PI_SESSIONS_DIR")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			t.Skip("no home dir")
		}
		root = filepath.Join(home, ".pi", "agent", "sessions")
	}
	dirs, err := os.ReadDir(root)
	if err != nil {
		t.Skipf("no sessions dir: %v", err)
	}
	parsed := 0
	for _, d := range dirs {
		if !d.IsDir() {
			continue
		}
		files, _ := os.ReadDir(filepath.Join(root, d.Name()))
		for _, fn := range files {
			if !strings.HasSuffix(fn.Name(), ".jsonl") {
				continue
			}
			f, err := os.Open(filepath.Join(root, d.Name(), fn.Name()))
			if err != nil {
				continue
			}
			h, err := ParseHeader(f)
			f.Close()
			if err != nil {
				continue
			}
			if h.Type != "session" {
				t.Fatalf("%s: wrong header type %q", fn.Name(), h.Type)
			}
			parsed++
		}
	}
	if parsed == 0 {
		t.Skip("no session files found")
	}
	t.Logf("parsed %d real session headers", parsed)
}
