package pi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// imageResultLine builds a chained read-tool result carrying a text
// block plus an image block with the given base64 payload and mime.
func imageResultLine(id, parent, data, mime string) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	return `{"type":"message","id":"` + id + `","parentId":` + p + `,"timestamp":"2026-10-10T12:00:03.000Z","message":{"role":"toolResult","toolName":"read","content":[{"type":"text","text":"[image read]"},{"type":"image","data":"` + data + `","mimeType":"` + mime + `"}],"timestamp":1760000003000}}`
}

func TestToolResultCountsImages(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeSession(t, path, headerLine, msgLine("a", "", "first"),
		imageResultLine("b", "a", "aGVsbG8=", "image/png"))

	ix := NewIndex(dir)
	_, items, err := ix.Transcript("s1", 0)
	if err != nil {
		t.Fatal(err)
	}
	var tr *ViewItem
	for i := range items {
		if items[i].Kind == "tool_result" {
			tr = &items[i]
		}
	}
	if tr == nil {
		t.Fatal("no tool_result item")
	}
	if tr.Images != 1 {
		t.Errorf("Images = %d, want 1", tr.Images)
	}
	if tr.Text != "[image read]" {
		t.Errorf("Text = %q, want the text block kept", tr.Text)
	}
}

func TestItemImage(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	// b: user text; c: read result with the image. Item seqs: 0=user, 1=tool_result.
	writeSession(t, path, headerLine, msgLine("b", "", "first"),
		imageResultLine("c", "b", "aGVsbG8=", "image/png"))

	ix := NewIndex(dir)
	data, mime, err := ix.ItemImage("s1", 1)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "hello" {
		t.Errorf("data = %q, want decoded %q", data, "hello")
	}
	if mime != "image/png" {
		t.Errorf("mime = %q, want image/png", mime)
	}

	if _, _, err := ix.ItemImage("s1", 0); err == nil {
		t.Error("text-only item: want an error")
	} else if !strings.Contains(err.Error(), "no image") {
		t.Errorf("text-only item error = %v, want a no-image cause", err)
	}
	if _, _, err := ix.ItemImage("s1", 99); err == nil {
		t.Error("unknown seq: want an error")
	}
}
