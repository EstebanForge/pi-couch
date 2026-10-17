package server

import (
	"bufio"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

const menuScreen = "Allow edit?  ❯ Yes  No  ↑↓ navigate enter select escape/ctrl+c cancel"

// herdrFake answers agent.list, pane.read and pane.send_input like a
// herdr socket driving the fixture session; *sessPath is read per
// request so the test can wire it after the socket exists. *sends
// counts send_input calls; failFirst makes exactly the first
// send_input return an error.
// fakeHerdrOpts configures the in-process herdr socket double; zero
// value suffices for the common read-only flow.
type fakeHerdrOpts struct {
	sessPath       *string
	sends          *int32  // pane.send_input count
	prompts        *int32  // agent.prompt count
	lastPrompt     *string // agent.prompt target name
	failFirstSend  bool
	failFirstStart bool
	status         *string     // agent_status in agent.list; blocked when nil
	paneText       *string     // pane.read text; the menu fixture when nil
	paneAfterSend  *string     // pane.read text once a send_input landed
	paneSeq        []string    // per-send states: after send n, serve paneSeq[min(n-1, len-1)]
	lastText       *string     // last send_input text param
	keysLog        *[][]string // every non-empty send_input keys call
}

func herdrFake(t *testing.T, o fakeHerdrOpts) string {
	var startFails int32
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "herdr.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				var m struct {
					ID     string          `json:"id"`
					Method string          `json:"method"`
					Params json.RawMessage `json:"params"`
				}
				if json.Unmarshal(line, &m) != nil {
					return
				}
				var resp string
				switch m.Method {
				case "agent.list":
					agentStatus := "blocked"
					if o.status != nil {
						agentStatus = *o.status
					}
					b, _ := json.Marshal(*o.sessPath)
					resp = `{"id":"x","result":{"agents":[{"name":"pi","agent":"pi","agent_status":"` + agentStatus + `","pane_id":"p1","workspace_id":"w1","terminal_title_stripped":"pi - s1:running...","agent_session":{"kind":"path","value":` + string(b) + `}}]}}`
				case "pane.split":
					resp = `{"id":"x","result":{"type":"pane_info","pane":{"pane_id":"p1","workspace_id":"w1"}}}`
				case "workspace.list":
					resp = `{"id":"x","result":{"type":"workspace_list","workspaces":[{"workspace_id":"w1","label":"Forge","number":1}]}}`
				case "agent.start":
					if o.failFirstStart && atomic.CompareAndSwapInt32(&startFails, 0, 1) {
						resp = `{"id":"x","error":{"message":"agent target pane p1 is not an available shell"}}`
					} else {
						resp = `{"id":"x","result":{"type":"ok"}}`
					}
				case "agent.prompt":
					var p struct {
						Target string `json:"target"`
					}
					_ = json.Unmarshal(m.Params, &p)
					if o.prompts != nil {
						atomic.AddInt32(o.prompts, 1)
					}
					if o.lastPrompt != nil {
						*o.lastPrompt = p.Target
					}
					resp = `{"id":"x","result":{"type":"ok"}}`
				case "pane.read":
					pane := menuScreen
					if o.paneText != nil {
						pane = *o.paneText
					}
					sent := int(atomic.LoadInt32(o.sends))
					if len(o.paneSeq) > 0 && sent > 0 {
						idx := sent - 1
						if idx >= len(o.paneSeq) {
							idx = len(o.paneSeq) - 1
						}
						pane = o.paneSeq[idx]
					} else if o.paneAfterSend != nil && sent > 0 {
						pane = *o.paneAfterSend
					}
					// json.Marshal escapes the newlines a multi-line
					// screen carries; raw ones would break the reply.
					paneJSON, _ := json.Marshal(pane)
					resp = `{"id":"x","result":{"read":{"pane_id":"p1","source":"screen","format":"ansi","text":` + string(paneJSON) + `,"revision":1}}}`
				case "pane.send_input":
					if o.keysLog != nil {
						var p struct {
							Keys []string `json:"keys"`
						}
						_ = json.Unmarshal(m.Params, &p)
						if len(p.Keys) > 0 {
							*o.keysLog = append(*o.keysLog, p.Keys)
						}
					}
					if o.lastText != nil {
						var p struct {
							Text string `json:"text"`
						}
						_ = json.Unmarshal(m.Params, &p)
						if p.Text != "" {
							*o.lastText = p.Text
						}
					}
					n := atomic.AddInt32(o.sends, 1)
					if o.failFirstSend && n == 1 {
						resp = `{"id":"x","error":{"message":"pane gone"}}`
					} else {
						resp = `{"id":"x","result":{"type":"ok"}}`
					}
				default:
					resp = `{"id":"x","result":{"type":"ok"}}`
				}
				_, _ = conn.Write([]byte(resp + "\n"))
			}()
		}
	}()
	return path
}

func streamTestAppWithHerdr(t *testing.T, sock string) (*app, string, http.Handler) {
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
	a := &app{
		logger:     slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:        pi.NewIndex(root),
		herdr:      herdr.NewAt(sock),
		watcher:    w,
		answered:   newDialogMemos(),
		reveals:    newRevealStore(),
		models:     newModelsCache(filepath.Join(t.TempDir(), "models.json")),
		dialogWake: make(chan struct{}, 1),
	}
	return a, sub, a.guard(a.routes())
}

func bridgeReal(t *testing.T, a *app, sessPath string) {
	t.Helper()
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{Kind: "confirm", Title: "Allow?", ID: "real"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()
}

// The double-tap: the second answer for the same dialog id must be
// refused without a second Enter, because the first Enter may already
// sit in the pane and would answer whatever pi shows next.
func TestDialogAnswerMemoDedupesDoubleTap(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends})
	a, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	bridgeReal(t, a, sessPath)

	answer := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer",
			strings.NewReader(`{"id":"real","answer":"yes"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := answer(); code != http.StatusOK {
		t.Fatalf("first answer: code=%d, want 200", code)
	}
	if n := atomic.LoadInt32(&sends); n != 1 {
		t.Fatalf("sends=%d, want 1", n)
	}
	if code := answer(); code != http.StatusConflict {
		t.Fatalf("second answer: code=%d, want 409", code)
	}
	if n := atomic.LoadInt32(&sends); n != 1 {
		t.Fatalf("sends=%d after dup, want still 1", n)
	}
}

// A send that provably failed releases the claim: the user may retry
// once herdr is healthy again and must not hit the memo wall.
func TestDialogAnswerMemoReleasesOnSendFailure(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, failFirstSend: true})
	a, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	bridgeReal(t, a, sessPath)

	post := func() int {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer",
			strings.NewReader(`{"id":"real","answer":"yes"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	if code := post(); code != http.StatusBadGateway {
		t.Fatalf("failing send: code=%d, want 502", code)
	}
	if code := post(); code != http.StatusOK {
		t.Fatalf("retry after failure: code=%d, want 200", code)
	}
	if n := atomic.LoadInt32(&sends); n != 2 {
		t.Fatalf("sends=%d, want 2", n)
	}
}
