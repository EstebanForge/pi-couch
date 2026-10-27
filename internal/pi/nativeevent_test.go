package pi

import (
	"encoding/json"
	"strings"
	"testing"
)

func nativeLine(t *testing.T, data map[string]any) []byte {
	t.Helper()
	raw, err := json.Marshal(map[string]any{
		"type": "custom", "customType": "agy-native-event", "data": data,
		"timestamp": "2026-10-27T11:20:59.324Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestNativeEventItems(t *testing.T) {
	e := parseEntry(t, nativeLine(t, map[string]any{
		"name": "execute", "status": "completed", "command": "grep -rn \"tools\"\n/home/x/a.js",
	}))
	vs := nativeEventItems(e)
	if len(vs) != 1 {
		t.Fatalf("items=%d", len(vs))
	}
	it := vs[0]
	if it.Kind != "tool_call" || it.ToolName != "execute" {
		t.Fatalf("kind=%s tool=%s", it.Kind, it.ToolName)
	}
	if it.Text != "grep -rn \"tools\"" {
		t.Fatalf("text=%q", it.Text)
	}
	if !strings.Contains(it.Body, "/home/x/a.js") {
		t.Fatalf("body=%q", it.Body)
	}
	if it.Time == 0 {
		t.Fatalf("time not parsed")
	}

	// path wins over command in the collapsed detail; the body carries
	// path, diff and output like the bridge's own renderer.
	e = parseEntry(t, nativeLine(t, map[string]any{
		"name": "edit_file", "status": "completed", "path": "/home/x/deep/b.ts",
		"diff": "+line one\n-line two", "output": "edited ok",
	}))
	it = nativeEventItems(e)[0]
	if it.Text != "b.ts" {
		t.Fatalf("text=%q", it.Text)
	}
	for _, want := range []string{"/home/x/deep/b.ts", "+line one", "-line two"} {
		if !strings.Contains(it.Body, want) {
			t.Fatalf("body=%q missing %q", it.Body, want)
		}
	}
	// With a diff present the output is suppressed, mirroring the
	// bridge's renderer (the diff is the result).
	if strings.Contains(it.Body, "edited ok") {
		t.Fatalf("body=%q carries output beside a diff", it.Body)
	}
	if strings.Contains(it.Body, "\n") == false {
		t.Fatalf("body=%q lost line structure", it.Body)
	}

	// no path, no command: the output's first line is the detail.
	e = parseEntry(t, nativeLine(t, map[string]any{
		"name": "read", "status": "completed", "output": "Read first clipboard image",
	}))
	if it = nativeEventItems(e)[0]; it.Text != "Read first clipboard image" {
		t.Fatalf("text=%q", it.Text)
	}

	// other custom types stay invisible.
	e = parseEntry(t, nativeLine(t, map[string]any{"name": "x"}))
	e.CustomType = "om.observations.recorded"
	if vs = nativeEventItems(e); vs != nil {
		t.Fatalf("non-agy custom produced items")
	}

	// An output that restates the tool name loses the duplicate.
	e = parseEntry(t, nativeLine(t, map[string]any{
		"name": "AskClaude", "status": "completed", "output": "AskClaude peer review",
	}))
	if it = nativeEventItems(e)[0]; it.Text != "peer review" {
		t.Fatalf("text=%q", it.Text)
	}

	// Poll runs ride the same collapsed-row path; the body carries the
	// renderer's expanded line (and marks the call complete for the
	// chat's live-row logic).
	raw, err := json.Marshal(map[string]any{
		"type": "custom", "customType": "agy-poll-run",
		"data": map[string]any{"target": "AskClaude", "count": 2, "state": "done",
			"firstAt": "2026-10-27T11:39:59.198Z", "lastAt": "2026-10-27T11:40:10.198Z"},
		"timestamp": "2026-10-27T11:39:59.198Z",
	})
	if err != nil {
		t.Fatal(err)
	}
	it = nativeEventItems(parseEntry(t, raw))[0]
	if it.ToolName != "bridge_poll_result" || it.Text != "Poll AskClaude (x2)" {
		t.Fatalf("poll tool=%s text=%q", it.ToolName, it.Text)
	}
	if !strings.Contains(it.Body, "2 polls") {
		t.Fatalf("poll body=%q", it.Body)
	}

	// A malformed data field degrades to a bare named row, never a
	// vanished entry.
	raw, err = json.Marshal(map[string]any{
		"type": "custom", "customType": "agy-native-event",
		"data": map[string]any{"name": "read", "output": map[string]any{"nested": true}},
	})
	if err != nil {
		t.Fatal(err)
	}
	it = nativeEventItems(parseEntry(t, raw))[0]
	if it.Kind != "tool_call" || it.ToolName != "read" {
		t.Fatalf("malformed kind=%s tool=%s", it.Kind, it.ToolName)
	}

	// A failed event shows its output beside the diff.
	e = parseEntry(t, nativeLine(t, map[string]any{
		"name": "edit_file", "status": "failed", "path": "/x/c.ts",
		"diff": "-old", "output": "permission denied",
	}))
	it = nativeEventItems(e)[0]
	if !strings.Contains(it.Body, "permission denied") {
		t.Fatalf("failed body=%q", it.Body)
	}

	// A command that starts with its own name keeps it: only the output
	// branch dedupes.
	e = parseEntry(t, nativeLine(t, map[string]any{
		"name": "execute", "status": "completed", "command": "execute build.sh",
	}))
	if it = nativeEventItems(e)[0]; it.Text != "execute build.sh" {
		t.Fatalf("text=%q", it.Text)
	}
}

func TestThinkingStripANSI(t *testing.T) {
	const raw = "\x1b[38;2;167;152;215mThinking:\x1b[39m \x1b[38;2;150;160;164mThe bridge dumps images.\x1b[39m"
	e := parseEntry(t, []byte(`{"type":"message","message":{"role":"assistant","content":[{"type":"thinking","thinking":"`+
		`\u001b[38;2;167;152;215mThinking:\u001b[39m \u001b[38;2;150;160;164mThe bridge dumps images.\u001b[39m`+`"}]}}`))
	vs := messageItems(e, 400)
	if len(vs) != 1 || vs[0].Kind != "thinking" {
		t.Fatalf("items=%+v", vs)
	}
	if vs[0].Text != "The bridge dumps images." {
		t.Fatalf("text=%q", vs[0].Text)
	}
	if strings.Contains(vs[0].Text, raw[:1]) {
		t.Fatalf("escape survived")
	}
}

func parseEntry(t *testing.T, line []byte) Entry {
	t.Helper()
	var e Entry
	if err := json.Unmarshal(line, &e); err != nil {
		t.Fatal(err)
	}
	return e
}
