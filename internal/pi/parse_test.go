package pi

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

// ParseHeader must stop reading once the header is found: parse() used to
// pay a whole extra file scan per meta miss because parseLines ran to EOF.
func TestParseHeaderStopsEarly(t *testing.T) {
	tail := strings.Repeat("{\"type\":\"message\",\"id\":\"x\",\"message\":{\"role\":\"assistant\",\"content\":\"filler\"}}\n", 4096)
	r := strings.NewReader(headerLine + "\n" + tail)
	h, err := ParseHeader(r)
	if err != nil {
		t.Fatal(err)
	}
	if h.Version != 3 {
		t.Fatalf("bad header: %+v", h)
	}
	if r.Len() == 0 {
		t.Fatal("ParseHeader read the whole file; it must stop at the header")
	}
}

// The single-pass parse must behave like the old two-pass shape: the
// header line still counts as an entry, and a header that follows a
// garbage line is still found.
func TestParseAllParity(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	garbage := `{"type":"?","id":"not-a-session"}`
	writeSession(t, path, garbage, headerLine, msgLine("a", "", "first"))

	ix := NewIndex(dir)
	meta, ok := ix.Meta("s1")
	if !ok {
		t.Fatal("Meta missed a session whose header follows a garbage line")
	}
	if meta.Version != 3 || meta.Cwd != "/tmp/proj" {
		t.Fatalf("bad header fields: %+v", meta)
	}
	// Parity with the old two-pass shape: every parseable line counts as
	// an entry, including the header line AND a decodeable unknown-type
	// line. Only unparseable lines are dropped.
	if meta.Entries != 3 {
		t.Fatalf("Entries=%d, want 3", meta.Entries)
	}
}

// The tail cache serves reads outside the global mutex, so concurrent
// Meta and Transcript calls must all succeed (run under -race in the
// suite): the lock is held only for the cache check and commit, never
// across the file read or parse.
func TestTailEntriesConcurrentReaders(t *testing.T) {
	dir := t.TempDir()
	sub := filepath.Join(dir, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	lines := []string{headerLine}
	for i := 0; i < 200; i++ {
		lines = append(lines, msgLine(fmt.Sprintf("e%d", i), "", "growing"))
	}
	writeSession(t, path, lines...)

	ix := NewIndex(dir)
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				if _, _, err := ix.Transcript("s1", 1000); err != nil {
					errs <- err
					return
				}
				if _, ok := ix.Meta("s1"); !ok {
					errs <- fmt.Errorf("Meta missed")
					return
				}
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
}
