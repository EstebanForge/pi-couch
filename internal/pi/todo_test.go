package pi

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// todoLine is an rpiv-todo-state custom entry, the extension's real
// jsonl shape: data.tasks carries the full checklist every time.
func todoLine(id, parent, tasks string) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	return `{"type":"custom","id":"` + id + `","parentId":` + p + `,"timestamp":"2026-10-10T12:00:02.000Z","customType":"rpiv-todo-state","data":{"action":"update","tasks":` + tasks + `,"nextId":3}}`
}

const twoTasks = `[{"id":1,"subject":"First task","status":"completed","activeForm":"Firsting"},{"id":2,"subject":"Second task","status":"in_progress","activeForm":"Seconding"}]`

func TestMetaTodosLatestWins(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	older := `[{"id":1,"subject":"Stale","status":"pending"}]`
	writeSession(t, path, headerLine, msgLine("a", "", "first"),
		todoLine("t1", "a", older), todoLine("t2", "t1", twoTasks))

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("meta not found")
	}
	if len(meta.Todos) != 2 {
		t.Fatalf("todos len=%d, want 2 (latest entry wins)", len(meta.Todos))
	}
	if meta.Todos[0].Subject != "First task" || meta.Todos[0].Status != "completed" {
		t.Errorf("todos[0] = %+v", meta.Todos[0])
	}
	if !slices.Contains([]string{"completed", "in_progress"}, meta.Todos[1].Status) {
		t.Errorf("todos[1].Status = %q", meta.Todos[1].Status)
	}
}

func TestMetaNoTodos(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine, msgLine("a", "", "first"))

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("meta not found")
	}
	if len(meta.Todos) != 0 {
		t.Errorf("todos = %+v, want empty", meta.Todos)
	}
}
