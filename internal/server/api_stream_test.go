package server

import (
	"bufio"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

// --- guard: the JSON reads must resist a rebound host too ---

func TestGuardBlocksReboundHostOnJSONReads(t *testing.T) {
	h := guardApp("https://box.example.ts.net")
	for _, path := range []string{"/api/sessions", "/stream/transcript/k", "/api/models"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "evil.example"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Fatalf("GET %s rebound host: %d, want 403", path, rec.Code)
		}
	}
	// Plain pages and healthz stay open to any host (loopback bind is the
	// boundary; the tailnet serves them under the allowed origin).
	for _, path := range []string{"/", "/healthz", "/sessions/k"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Host = "evil.example"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("GET %s must not be host-gated", path)
		}
	}
	// The same JSON reads pass under loopback and allowed-origin hosts.
	for _, host := range []string{"127.0.0.1:8090", "box.example.ts.net"} {
		req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
		req.Host = host
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusForbidden {
			t.Fatalf("GET /api/sessions host %s must pass", host)
		}
	}
}

// --- stream harness ---

const sessHeader = `{"type":"session","version":3,"id":"11111111-2222-3333-4444-555555555555","timestamp":"2026-10-10T12:00:00.000Z","cwd":"/tmp/proj"}`

// userLine is a parent-chained user message entry, the real jsonl shape.
func userLine(id, parent, text string) string {
	p := "null"
	if parent != "" {
		p = `"` + parent + `"`
	}
	return `{"type":"message","id":"` + id + `","parentId":` + p + `,"timestamp":"2026-10-10T12:00:01.000Z","message":{"role":"user","content":"` + text + `","timestamp":1760000001000}}`
}

func streamTestApp(t *testing.T) (*app, string, http.Handler) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeLines(t, path, sessHeader, userLine("a", "", "first"))
	w, err := pi.StartWatcher(t.Context(), root)
	if err != nil {
		t.Fatal(err)
	}
	_ = w
	a := &app{
		logger:  slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:     pi.NewIndex(root),
		herdr:   herdr.New(),
		watcher: w,
	}
	return a, sub, a.guard(a.routes())
}

func writeLines(t *testing.T, path string, lines ...string) {
	t.Helper()
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0o644)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, l := range lines {
		if _, err := f.WriteString(l + "\n"); err != nil {
			t.Fatal(err)
		}
	}
}

type frame struct {
	name string
	data string
}

// readFrames pumps SSE frames into a channel so tests can select with
// timeouts instead of hanging on a never-ending body.
func readFrames(t *testing.T, r io.Reader) <-chan frame {
	t.Helper()
	out := make(chan frame, 16)
	go func() {
		defer close(out)
		br := bufio.NewReader(r)
		var name, data string
		for {
			line, err := br.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\n")
			switch {
			case line == "":
				if name != "" {
					out <- frame{name, data}
				}
				name, data = "", ""
			case strings.HasPrefix(line, "event: "):
				name = strings.TrimPrefix(line, "event: ")
			case strings.HasPrefix(line, "data: "):
				data = strings.TrimPrefix(line, "data: ")
			}
		}
	}()
	return out
}

func waitFrame(t *testing.T, ch <-chan frame, timeout time.Duration, name string) frame {
	t.Helper()
	select {
	case f, ok := <-ch:
		if !ok {
			t.Fatalf("stream ended before %s", name)
		}
		if f.name != name {
			t.Fatalf("got event %q, want %q", f.name, name)
		}
		return f
	case <-time.After(timeout):
		t.Fatalf("timed out waiting for %s", name)
		return frame{}
	}
}

func TestJSONTranscriptStream(t *testing.T) {
	_, sub, h := streamTestApp(t)
	path := filepath.Join(sub, "s1.jsonl")
	srv := httptest.NewServer(h)
	defer srv.Close()

	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/stream/transcript/s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	frames := readFrames(t, resp.Body)
	defer resp.Body.Close()

	// Reset comes first, with the full tail and the status triple.
	f := waitFrame(t, frames, 5*time.Second, "reset")
	var rs transcriptReset
	if err := json.Unmarshal([]byte(f.data), &rs); err != nil {
		t.Fatal(err)
	}
	if len(rs.Items) != 1 || rs.Items[0].Seq != 0 {
		t.Fatalf("reset items=%+v", rs.Items)
	}

	// Growth arrives as append with continuing seqs.
	writeLines(t, path, userLine("b", "a", "second"))
	f = waitFrame(t, frames, 8*time.Second, "append")
	var ap transcriptAppend
	if err := json.Unmarshal([]byte(f.data), &ap); err != nil {
		t.Fatal(err)
	}
	if len(ap.Items) != 1 || ap.Items[0].Seq != 1 {
		t.Fatalf("append items=%+v", ap.Items)
	}

	// A branch switch is a fork APPENDED to the jsonl: same item count,
	// different prefix. The fingerprints must catch it and re-reset, never
	// silently append on top of a stale view.
	writeLines(t, path, userLine("c", "a", "branched"))
	f = waitFrame(t, frames, 8*time.Second, "reset")
	var rs2 transcriptReset
	if err := json.Unmarshal([]byte(f.data), &rs2); err != nil {
		t.Fatal(err)
	}
	if len(rs2.Items) != 2 || rs2.Items[1].Text != "branched" {
		t.Fatalf("reset after branch items=%+v", rs2.Items)
	}
}

func TestJSONStopClose(t *testing.T) {
	_, _, h := streamTestApp(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	// No herdr agent drives the fixture's temp path: the JSON twins must
	// say so in-band (fetch cannot follow the templ 303s).
	for _, tc := range []struct{ route, want string }{
		{"/api/sessions/s1/stop", "notdriven"},
		{"/api/sessions/s1/close", "notdriven"},
	} {
		resp, err := http.Post(srv.URL+tc.route, "", nil)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusConflict {
			t.Fatalf("%s: %d, want 409", tc.route, resp.StatusCode)
		}
		var out struct {
			Error string `json:"error"`
		}
		if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
			t.Fatal(err)
		}
		if out.Error != tc.want {
			t.Fatalf("%s: error=%q, want %q", tc.route, out.Error, tc.want)
		}
	}

	resp, err := http.Post(srv.URL+"/api/sessions/nope/stop", "", nil)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotFound {
		t.Fatalf("unknown key: %d, want 404", resp.StatusCode)
	}
}

func TestJSONSessionsStream(t *testing.T) {
	_, sub, h := streamTestApp(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stream/sessions")
	if err != nil {
		t.Fatal(err)
	}
	frames := readFrames(t, resp.Body)
	defer resp.Body.Close()

	f := waitFrame(t, frames, 5*time.Second, "reset")
	var snap sessionsWire
	if err := json.Unmarshal([]byte(f.data), &snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Sessions) != 1 || snap.Sessions[0].Key != "s1" {
		t.Fatalf("snapshot sessions=%+v", snap.Sessions)
	}

	// A new session file must arrive as a second reset with both sessions.
	writeLines(t, filepath.Join(sub, "s2.jsonl"), sessHeader)
	f = waitFrame(t, frames, 8*time.Second, "reset")
	var snap2 sessionsWire
	if err := json.Unmarshal([]byte(f.data), &snap2); err != nil {
		t.Fatal(err)
	}
	if len(snap2.Sessions) != 2 {
		t.Fatalf("second snapshot sessions=%d, want 2", len(snap2.Sessions))
	}
}
