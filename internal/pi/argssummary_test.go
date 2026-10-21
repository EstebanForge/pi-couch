package pi

import "testing"

// Tool rows in the chat read "Edited src/a.tsx" or "Ran npm test", so
// the summary must carry the argument that names what the tool did —
// a path, a command — not the argument key names.
func TestArgsSummaryPicksMeaningfulValue(t *testing.T) {
	cases := []struct {
		name string
		raw  string
		want string
	}{
		{"path wins", `{"path":"src/a.tsx","other":1}`, "src/a.tsx"},
		{"file_path alias", `{"file_path":"web/x.ts"}`, "web/x.ts"},
		{"command", `{"command":"npm test","timeout":9000}`, "npm test"},
		{"url", `{"url":"https://x.dev/a"}`, "https://x.dev/a"},
		{"no known key joins pairs", `{"kind":"wiki","limit":5}`, "kind: wiki, limit: 5"},
		{"content-heavy pairs cap", `{"body":"` + string(make([]byte, 300)) + `","n":1}`, ""},
		{"empty", ``, ""},
	}
	for _, c := range cases {
		got := argsSummary([]byte(c.raw))
		if c.name == "content-heavy pairs cap" {
			if len(got) > 121 {
				t.Fatalf("%s: %d runes, capped at 120", c.name, len([]rune(got)))
			}
			continue
		}
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// The peer-delegation tools carry their task in prompt; the chat row
// should read the task head, not "model: x, prompt: y".
func TestArgsSummaryPrefersPrompt(t *testing.T) {
	got := argsSummary([]byte(`{"model":"gemini","thinking":"high","prompt":"READ-AND-REPORT ONLY"}`))
	if got != "READ-AND-REPORT ONLY" {
		t.Fatalf("got %q", got)
	}
}
