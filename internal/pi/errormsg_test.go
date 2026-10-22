package pi

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"unicode/utf8"
)

func errLine(id, parent, msg string) string {
	return `{"type":"message","id":"` + id + `","parentId":"` + parent + `","timestamp":"2026-10-10T12:00:02.000Z","message":{"role":"assistant","content":[],"api":"zai","provider":"zai","model":"glm-5.3-flash","usage":{"input":1,"output":0,"totalTokens":1,"cost":{"total":0}},"stopReason":"error","errorMessage":` + strconv.Quote(msg) + `,"timestamp":1760000002000}}`
}

func errThinkingLine(id, parent, msg string) string {
	return `{"type":"message","id":"` + id + `","parentId":"` + parent + `","timestamp":"2026-10-10T12:00:03.000Z","message":{"role":"assistant","content":[{"type":"thinking","thinking":""}],"api":"zai","provider":"zai","model":"glm-5.3-flash","usage":{"input":1,"output":0,"totalTokens":1,"cost":{"total":0}},"stopReason":"error","errorMessage":` + strconv.Quote(msg) + `,"timestamp":1760000003000}}`
}

func TestTranscriptSurfacesAPIError(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := sub + "/s1.jsonl"
	const blob = `429 {"type":"error","error":{"type":"rate_limit_error","code":"1308","message":"[1308][Usage limit reached for 5 hour. Your limit will reset at 2026-10-22 09:58:51]"},"request_id":"r1"}`
	writeSession(t, path,
		headerLine,
		msgLine("a", "", "hello"),
		errLine("b", "a", blob),
		msgLine("c", "b", "again"),
		errThinkingLine("d", "c", blob),
	)

	ix := NewIndex(dir)
	_, items, err := ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	var kinds []string
	for _, it := range items {
		kinds = append(kinds, it.Kind)
	}
	want := []string{"user", "error", "user", "thinking", "error"}
	if len(kinds) != len(want) {
		t.Fatalf("kinds=%v, want %v", kinds, want)
	}
	for i := range want {
		if kinds[i] != want[i] {
			t.Fatalf("kinds=%v, want %v", kinds, want)
		}
	}
	last := items[len(items)-1]
	if !strings.Contains(last.Text, "rate_limit_error") || !strings.Contains(last.Text, "Usage limit reached") {
		t.Fatalf("error text = %q", last.Text)
	}
}

func TestTranscriptAPIErrorTruncates(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := sub + "/s1.jsonl"
	long := strings.Repeat("x", 900)
	writeSession(t, path, headerLine, errLine("a", "", long))

	ix := NewIndex(dir)
	_, items, err := ix.Transcript("s1", 100)
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 || items[0].Kind != "error" {
		t.Fatalf("items=%+v", items)
	}
	if !items[0].Truncated || utf8.RuneCountInString(items[0].Text) > 400 {
		t.Fatalf("truncated=%v runes=%d", items[0].Truncated, utf8.RuneCountInString(items[0].Text))
	}
}
