package server

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

// hcFakeCore serves hc's JSON-RPC over a unix socket: one reply per
// request line, every connection held open so the driver's subscribe
// never drops. Records keys presses and screen replies for assertions.
type hcFakeCore struct {
	t    *testing.T
	sock string

	sessionPath string

	mu     sync.Mutex
	keys   [][]string
	paints []string // terminal.read sequence; the last repeats
	ri     int

	subs     []net.Conn              // connections that subscribed, in order
	runs     []map[string]any        // terminal.run params
	sends    []map[string]any        // terminal.send params
	kills    []string                // terminal.kill addresses
	attaches []map[string]any        // framed terminal.attach params
	writes   [][]byte                // framed kind-6 payloads, in order
	docs     map[string]any          // doc.get values keyed by name
	state    string                  // snapshot status state; empty = working
	snap     func() []map[string]any // snapshot terminals; nil = labeled t1

	// failKeysNext fails this many terminal.keys calls (send failures,
	// memo release tests); 0 means all succeed.
	failKeysNext atomic.Int32
}

// writerText joins the recorded kind-6 payloads; the raw input a
// writer attach delivered.
func (f *hcFakeCore) writerText() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var b []byte
	for _, w := range f.writes {
		b = append(b, w...)
	}
	return string(b)
}

func (f *hcFakeCore) attachParams() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.attaches...)
}

// fakeFrame encodes hc's 9-byte framed header (u32 LE channel, kind
// byte, u32 LE length) plus payload; encodeFrame stays unexported in
// internal/hc.
func fakeFrame(ch uint32, kind byte, payload []byte) []byte {
	head := make([]byte, 9)
	binary.LittleEndian.PutUint32(head[0:4], ch)
	head[4] = kind
	binary.LittleEndian.PutUint32(head[5:9], uint32(len(payload)))
	return append(head, payload...)
}

// startHCFake wires a fake core whose snapshot carries one live
// terminal labeled with the session path and whose screen walks the
// given paints (the last repeats). Returns the driver over it.
func startHCFake(t *testing.T, path string, paints ...string) (*hc.Driver, *hcFakeCore) {
	t.Helper()
	f := &hcFakeCore{t: t, paints: paints, sessionPath: path, docs: map[string]any{}}
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "hc.sock")
	ln, err := net.Listen("unix", sockPath)
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
			go f.serve(conn)
		}
	}()
	f.sock = sockPath

	drv := hc.NewDriver(hc.NewAt(sockPath))
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go drv.Run(ctx)
	return drv, f
}

func (f *hcFakeCore) serve(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	first, err := br.ReadString('\n')
	if err != nil {
		return
	}
	first = strings.TrimRight(first, "\n")
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params map[string]any  `json:"params"`
	}
	if json.Unmarshal([]byte(first), &req) == nil && req.Method == "hello" {
		if framed, _ := req.Params["framed"].(bool); framed {
			out, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "id": req.ID,
				"result": map[string]any{"generation": 1, "framed": true, "machine": "fake"},
			})
			_, _ = conn.Write(append(out, '\n'))
			f.framedServe(br, conn)
			return
		}
	}
	f.reply(conn, first)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		f.reply(conn, strings.TrimRight(line, "\n"))
	}
}

// framedServe serves one framed attach connection: the terminal.attach
// reply rides a frameData frame on channel 0, kind-6 payloads are the
// writer's raw input, credits are consumed and ignored.
func (f *hcFakeCore) framedServe(br *bufio.Reader, conn net.Conn) {
	for {
		head := make([]byte, 9)
		if _, err := io.ReadFull(br, head); err != nil {
			return
		}
		ch := binary.LittleEndian.Uint32(head[0:4])
		kind := head[4]
		size := binary.LittleEndian.Uint32(head[5:9])
		payload := make([]byte, size)
		if _, err := io.ReadFull(br, payload); err != nil {
			return
		}
		switch {
		case kind == 6:
			f.mu.Lock()
			f.writes = append(f.writes, payload)
			f.mu.Unlock()
		case kind == 0 && ch == 0:
			var req struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params map[string]any  `json:"params"`
			}
			if err := json.Unmarshal(payload, &req); err != nil {
				continue
			}
			switch req.Method {
			case "terminal.attach":
				f.mu.Lock()
				f.attaches = append(f.attaches, req.Params)
				f.mu.Unlock()
				out, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "id": req.ID,
					"result": map[string]any{
						"channel":         7,
						"automatic_input": true,
						"terminal":        map[string]any{"address": "t7", "state": "live", "cols": 131, "rows": 31},
					},
				})
				_, _ = conn.Write(fakeFrame(0, 0, out))
			default:
				out, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0", "id": req.ID,
					"error": map[string]any{"code": -32601, "message": "fake: framed method not implemented"},
				})
				_, _ = conn.Write(fakeFrame(0, 0, out))
			}
		}
	}
}

func (f *hcFakeCore) reply(conn net.Conn, line string) {
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params map[string]any  `json:"params"`
	}
	if err := json.Unmarshal([]byte(line), &req); err != nil {
		return
	}
	var result any
	switch req.Method {
	case "events.subscribe":
		f.mu.Lock()
		f.subs = append(f.subs, conn)
		f.mu.Unlock()
		result = map[string]any{"seq": 1, "terminals": f.snapshotTerminals()}
	case "terminal.run":
		f.mu.Lock()
		f.runs = append(f.runs, req.Params)
		f.mu.Unlock()
		name, _ := req.Params["name"].(string)
		result = map[string]any{"address": "t7", "name": name, "state": "live"}
	case "terminal.read":
		f.mu.Lock()
		text := f.paints[f.ri]
		if f.ri < len(f.paints)-1 {
			f.ri++
		}
		f.mu.Unlock()
		result = map[string]any{"text": text, "complete": true, "how": "screen"}
	case "doc.get":
		name, _ := req.Params["name"].(string)
		f.mu.Lock()
		v := f.docs[name]
		f.mu.Unlock()
		result = map[string]any{"value": v, "version": 7}
	case "terminal.kill":
		addr, _ := req.Params["terminal"].(string)
		f.mu.Lock()
		f.kills = append(f.kills, addr)
		subs := append([]net.Conn(nil), f.subs...)
		f.mu.Unlock()
		// The driver's Close waits for the terminal to leave the live
		// set before escalating; the fake reports the kill as the core
		// would, with a removed event.
		for _, sc := range subs {
			line, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0",
				"method":  "event",
				"params":  map[string]any{"type": "removed", "terminal": addr, "data": map[string]any{}, "seq": 1},
			})
			_, _ = sc.Write(append(line, '\n'))
		}
		result = map[string]any{}
	case "terminal.rm":
		result = map[string]any{}
	case "terminal.keys":
		raw, _ := json.Marshal(req.Params["keys"])
		var keys []string
		_ = json.Unmarshal(raw, &keys)
		f.mu.Lock()
		failed := f.failKeysNext.Load() > 0
		if failed {
			f.failKeysNext.Add(-1)
		}
		addr, _ := req.Params["terminal"].(string)
		exitOnEOF := len(keys) == 1 && keys[0] == "ctrl+d"
		subs := append([]net.Conn(nil), f.subs...)
		if !failed {
			f.keys = append(f.keys, keys)
		}
		f.mu.Unlock()
		// pi exits on EOF: a graceful ctrl+d ends the terminal, the way
		// the real core reports it.
		if exitOnEOF {
			for _, sc := range subs {
				line, _ := json.Marshal(map[string]any{
					"jsonrpc": "2.0",
					"method":  "event",
					"params":  map[string]any{"type": "removed", "terminal": addr, "data": map[string]any{}, "seq": 1},
				})
				_, _ = sc.Write(append(line, '\n'))
			}
		}
		if failed {
			out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "error": map[string]any{"code": -32000, "message": "hc: keys lost"}})
			_, _ = conn.Write(append(out, '\n'))
			return
		}
		result = map[string]any{"status": map[string]any{"state": "working"}, "turn": 1}
	case "terminal.send":
		f.mu.Lock()
		f.sends = append(f.sends, req.Params)
		f.mu.Unlock()
		result = map[string]any{"started": true, "turn": 1}
	default:
		result = map[string]any{}
	}
	out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
	_, _ = conn.Write(append(out, '\n'))
}

func (f *hcFakeCore) snapshotTerminals() []map[string]any {
	f.mu.Lock()
	snap := f.snap
	state := f.state
	f.mu.Unlock()
	if state == "" {
		state = "working"
	}
	if snap != nil {
		return snap()
	}
	return []map[string]any{{
		"address":  "t1",
		"labels":   map[string]any{"pi.session": f.sessionPath},
		"state":    "live",
		"status":   map[string]any{"state": state, "app": "pi"},
		"title":    "Pi · t1",
		"reported": true,
	}}
}

// event pushes one notification onto the newest subscribe connection.
func (f *hcFakeCore) event(kind, addr string, data map[string]any) {
	f.mu.Lock()
	var conn net.Conn
	if n := len(f.subs); n > 0 {
		conn = f.subs[n-1]
	}
	f.mu.Unlock()
	if conn == nil {
		f.t.Fatal("no subscribe conn to push the event on")
	}
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params":  map[string]any{"type": kind, "terminal": addr, "data": data, "seq": 1},
	})
	if _, err := conn.Write(append(line, '\n')); err != nil {
		f.t.Fatalf("event: %v", err)
	}
}

func (f *hcFakeCore) runLog() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any{}, f.runs...)
}

func (f *hcFakeCore) sendLog() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any{}, f.sends...)
}

func (f *hcFakeCore) keyLog() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string{}, f.keys...)
}

func (f *hcFakeCore) killLog() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.kills...)
}

// hcDialogApp builds the driver=hc app literal: herdr nil, awaiting
// nil until the test wires the driver over the fake core.
func hcDialogApp(t *testing.T) (*app, string) {
	t.Helper()
	root := t.TempDir()
	sub := filepath.Join(root, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(sub, "s1.jsonl")
	writeLines(t, path, sessHeader, userLine("a", "", "first"))
	a := &app{
		logger:      slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:         pi.NewIndex(root),
		home:        testHome(),
		answered:    newDialogMemos(),
		models:      newModelsCache(filepath.Join(t.TempDir(), "models.json")),
		memory:      newMemoryProbe("", ""),
		bridge:      map[string]bridgeEntry{},
		dialogLatch: map[string]*dialogWire{},
	}
	return a, path
}

// wireHC points the app at the fake-backed driver, mirroring Run's
// wiring.
func wireHC(a *app, drv *hc.Driver) {
	a.hcDrv = drv
	a.awaiting = newAwaitingDetector(drv, time.Hour)
}

// waitForHC waits until the driver's session map resolves the path.
// Five seconds: under ci's parallel load the subscribe/reseed plus
// dispatch chain can outgrow a 2s window without any real hang.
func waitForHC(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatal("condition never held")
}

func hcAnswerReq(t *testing.T, a *app, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/dialog/answer", strings.NewReader(body))
	req.SetPathValue("key", "s1")
	rec := httptest.NewRecorder()
	a.apiDialogAnswer(rec, req)
	return rec
}

func plantBridge(t *testing.T, a *app, path string, d BridgeDialog) {
	t.Helper()
	a.bridgeMu.Lock()
	a.bridge[path] = bridgeEntry{dialog: d, expires: time.Now().Add(time.Minute)}
	a.bridgeMu.Unlock()
}

func TestHCDialogAnswerYes(t *testing.T) {
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, confirmPaint("Allow push?"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "real", Kind: "confirm", Title: "Allow push?"})

	rec := hcAnswerReq(t, a, `{"id":"real","answer":"yes"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s, want 200", rec.Code, rec.Body.String())
	}
	keys := fake.keyLog()
	if len(keys) != 1 || len(keys[0]) != 1 || keys[0][0] != "enter" {
		t.Fatalf("keys = %v, want one [enter]", keys)
	}
}

func TestHCDialogAnswerEscape(t *testing.T) {
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, confirmPaint("Allow push?"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "real", Kind: "confirm", Title: "Allow push?"})

	rec := hcAnswerReq(t, a, `{"id":"real","answer":"escape"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s, want 200", rec.Code, rec.Body.String())
	}
	keys := fake.keyLog()
	if len(keys) != 1 || len(keys[0]) != 1 || keys[0][0] != "esc" {
		t.Fatalf("keys = %v, want one [esc]", keys)
	}
}

func TestHCDialogAnswerEditorAcceptSendsEnter(t *testing.T) {
	// Accepting pi's editor from the phone is submit-as-is: the buffer
	// lives in pi, so yes is one enter. The card sends its served copy
	// back so the server can refuse when pi now paints a different
	// buffer under the same title. hc's send cannot rewrite a buffer
	// (it always appends Enter), so no text may go out.
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, editorPaint("feat: x\n\nbody line\n"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"yes","text":"feat: x\n\nbody line"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s, want 200", rec.Code, rec.Body.String())
	}
	if keys := fake.keyLog(); len(keys) != 1 || len(keys[0]) != 1 || keys[0][0] != "enter" {
		t.Fatalf("keys = %v, want exactly one enter", keys)
	}
	if sends := fake.sendLog(); len(sends) != 0 {
		t.Fatalf("sends = %v, want none (no rewrite)", sends)
	}
}

func TestHCDialogAnswerEditorTypeRewritesAndSubmits(t *testing.T) {
	// The type verb on an editor rewrites pi's buffer: per-line clear
	// keys, a raw paste, then a content match before the one enter.
	// The paint walk crosses the empty-editor state on the way.
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath,
		editorPaint("feat: old\n"),
		editorEmptyPaint(),
		editorPaint("feat: new\n"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"type","text":"feat: new"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s, want 200", rec.Code, rec.Body.String())
	}
	if got := fake.writerText(); got != "feat: new" {
		t.Fatalf("paste = %q, want the new text", got)
	}
	attaches := fake.attachParams()
	if len(attaches) != 1 || attaches[0]["observe"] != false {
		t.Fatalf("attach params = %v, want one writer attach", attaches)
	}
	keys := fake.keyLog()
	if len(keys) != 2 {
		t.Fatalf("keys = %v, want one clear iteration plus one enter", keys)
	}
	if len(keys[0]) != 3 || keys[0][0] != "ctrl+a" {
		t.Fatalf("clear keys = %v, want ctrl+a ctrl+k backspace", keys[0])
	}
	if len(keys[1]) != 1 || keys[1][0] != "enter" {
		t.Fatalf("submit keys = %v, want one enter", keys[1])
	}
	if sends := fake.sendLog(); len(sends) != 0 {
		t.Fatalf("sends = %v, want none (the rewrite is a paste, not a send)", sends)
	}
}

func TestHCDialogAnswerEditorTypeRefusesAnUnmovedScreen(t *testing.T) {
	// If the buffer never takes the text, the rewrite must not fire
	// its enter: pi would submit something the user did not see.
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, editorPaint("feat: old\n"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"type","text":"feat: new"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d, want 409 on a stuck screen", rec.Code)
	}
	for _, batch := range fake.keyLog() {
		if len(batch) == 1 && batch[0] == "enter" {
			t.Fatalf("keys contain an enter, want none on refusal")
		}
	}
	if got := fake.writerText(); got != "feat: new" {
		t.Fatalf("paste = %q, want the blind write recorded", got)
	}
}

func TestHCDialogAnswerEditorTypeRefusesScrolled(t *testing.T) {
	// A scrolled editor paints a tail: the clear count is unknown, so
	// the type verb refuses before touching the terminal.
	const paint = " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 4 more ────\n" +
		"tail line one\ntail line two\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, paint)
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"type","text":"feat: new"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d, want 409 for a scrolled editor", rec.Code)
	}
	if keys := fake.keyLog(); len(keys) != 0 {
		t.Fatalf("keys = %v, want none", keys)
	}
	if got := fake.writerText(); got != "" {
		t.Fatalf("paste = %q, want none", got)
	}
}

func TestHCDialogAnswerEditorTypeWasProofRefusesABufferSwap(t *testing.T) {
	// The type verb's was field is the card's content proof: a stale
	// card (or a same-title successor) must refuse before any clear
	// key touches pi, not after a blind rewrite.
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath,
		editorPaint("feat: current\n"),
		editorPaint("feat: current\n"),
		editorPaint("feat: current\n"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"type","text":"feat: new","was":"feat: stale"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d body %s, want 409", rec.Code, rec.Body.String())
	}
	if keys := fake.keyLog(); len(keys) != 0 {
		t.Fatalf("keys = %v, want none", keys)
	}
	if got := fake.writerText(); got != "" {
		t.Fatalf("paste = %q, want none", got)
	}
}

func TestHCDialogAnswerEditorTypeAbandonsWhenDialogGoes(t *testing.T) {
	// If the dialog leaves mid-rewrite (dismissed at the desk), the
	// remaining clears and the paste would land in pi's main editor:
	// the pre-paste re-read must abort the rewrite instead.
	// Paint walk: settle reads the two-line editor, the pre-paste
	// re-read finds a confirm dialog where the editor was.
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath,
		editorPaint("feat: two\nlines\n"),
		confirmPaint("Really quit?"),
		confirmPaint("Really quit?"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"type","text":"feat: new"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d body %s, want 409", rec.Code, rec.Body.String())
	}
	if got := fake.writerText(); got != "" {
		t.Fatalf("paste = %q, want none after the dialog left", got)
	}
	for _, k := range fake.keyLog() {
		if len(k) == 1 && k[0] == "enter" {
			t.Fatalf("keys = %v, want no submit", fake.keyLog())
		}
	}
}

func TestHCDialogAnswerEditorTypeRefusesTooLong(t *testing.T) {
	// Over the clear cap the rewrite would leave a half-cleared
	// buffer before refusing; refuse up front with pi untouched.
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, editorPaint(strings.Repeat("line\n", 65)))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"type","text":"feat: new"}`)
	if rec.Code != http.StatusConflict || !strings.Contains(rec.Body.String(), "editor too long") {
		t.Fatalf("code %d body %s, want 409 editor too long", rec.Code, rec.Body.String())
	}
	if keys := fake.keyLog(); len(keys) != 0 {
		t.Fatalf("keys = %v, want none", keys)
	}
	if got := fake.writerText(); got != "" {
		t.Fatalf("paste = %q, want none", got)
	}
}

func TestHCDialogAnswerEditorRefusesABufferSwap(t *testing.T) {
	// Two commit previews share a title: without a content check the
	// stale card would commit the successor buffer sight-unseen.
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, editorPaint("feat: y\n\nreplaced body\n"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"yes","text":"feat: x\n\nbody line"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d body %s, want 409", rec.Code, rec.Body.String())
	}
	if keys := fake.keyLog(); len(keys) != 0 {
		t.Fatalf("keys = %v, want none", keys)
	}
}

func TestHCDialogAnswerEditorScrolledAcceptsTitleOnly(t *testing.T) {
	// A scrolled editor serves no comparable copy - the painted body
	// is the tail - so acceptance rests on the identity gate alone.
	const paint = " Commit staged changes with this message?\n" +
		"────────────────────────────────────────────\n" +
		"──── ↑ 4 more ────\n" +
		"tail line one\ntail line two\n" +
		"────────────────────────────────────────────\n\n" +
		" enter submit  shift+enter/ctrl+j newline  escape/ctrl+c cancel  ctrl+g external editor"
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, paint)
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"yes"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s, want 200", rec.Code, rec.Body.String())
	}
	if keys := fake.keyLog(); len(keys) != 1 || keys[0][0] != "enter" {
		t.Fatalf("keys = %v, want one enter", keys)
	}
}

func TestHCDialogAnswerOptionWalksTheCursor(t *testing.T) {
	// The cursor starts on row 0; picking row 1 must walk down once and
	// verify the cursor before enter. The second paint is pi's redraw
	// with the cursor on blue.
	a, sessPath := hcDialogApp(t)
	red := "Pick one\n\n→ red\n  blue\n\n↑↓ navigate  enter select  escape/ctrl+c cancel\n"
	blue := "Pick one\n\n  red\n→ blue\n\n↑↓ navigate  enter select  escape/ctrl+c cancel\n"
	drv, fake := startHCFake(t, sessPath, red, blue)
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "sel", Kind: "select", Title: "Pick one"})

	rec := hcAnswerReq(t, a, `{"id":"sel","answer":"blue"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s, want 200", rec.Code, rec.Body.String())
	}
	keys := fake.keyLog()
	var downs, enters int
	for _, k := range keys {
		for _, key := range k {
			switch key {
			case "down":
				downs++
			case "enter":
				enters++
			}
		}
	}
	if downs != 1 || enters != 1 {
		t.Fatalf("keys = %v, want one down and one enter", keys)
	}
}

func TestHCAwaitingDetectorReadsTheScreen(t *testing.T) {
	a, sessPath := hcDialogApp(t)
	drv, _ := startHCFake(t, sessPath, confirmPaint("Allow push?"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(context.Background(), sessPath)
		return err == nil
	})

	a.awaiting.tick(context.Background())
	if !a.awaiting.isAwaiting(sessPath) {
		t.Fatal("confirm paint did not raise the awaiting cue")
	}
	if a.awaiting.parsed(sessPath) == nil {
		t.Fatal("confirm paint did not parse into a dialog")
	}
	if a.paneDialogID(sessPath) == "" {
		t.Fatal("pane dialog id missing under hc")
	}
}
