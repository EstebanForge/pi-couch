package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
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

	subs  []net.Conn              // connections that subscribed, in order
	runs  []map[string]any        // terminal.run params
	sends []map[string]any        // terminal.send params
	snap  func() []map[string]any // snapshot terminals; nil = labeled t1
}

// startHCFake wires a fake core whose snapshot carries one live
// terminal labeled with the session path and whose screen walks the
// given paints (the last repeats). Returns the driver over it.
func startHCFake(t *testing.T, path string, paints ...string) (*hc.Driver, *hcFakeCore) {
	t.Helper()
	f := &hcFakeCore{t: t, paints: paints, sessionPath: path}
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
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 2048)
	for {
		n, err := conn.Read(tmp)
		if err != nil {
			return
		}
		buf = append(buf, tmp[:n]...)
		for {
			i := strings.Index(string(buf), "\n")
			if i < 0 {
				break
			}
			line := string(buf[:i])
			buf = buf[i+1:]
			f.reply(conn, line)
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
	case "terminal.keys":
		raw, _ := json.Marshal(req.Params["keys"])
		var keys []string
		_ = json.Unmarshal(raw, &keys)
		f.mu.Lock()
		f.keys = append(f.keys, keys)
		f.mu.Unlock()
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
	f.mu.Unlock()
	if snap != nil {
		return snap()
	}
	return []map[string]any{{
		"address": "t1",
		"labels":  map[string]any{"pi.session": f.sessionPath},
		"state":   "live",
		"status":  map[string]any{"state": "working", "app": "pi"},
		"title":   "Pi · t1",
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

// wireHC points the app at the fake-backed driver, mirroring Run's hc
// branch.
func wireHC(a *app, drv *hc.Driver) {
	a.status = drv
	a.hcDrv = drv
	a.awaiting = newAwaitingDetector(drv, time.Hour)
}

func waitForHC(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
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
		_, err := a.statusFor().TargetForPath(context.Background(), sessPath)
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
		_, err := a.statusFor().TargetForPath(context.Background(), sessPath)
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

func TestHCDialogAnswerTypeRefusesEditor(t *testing.T) {
	// hc's terminal.send appends Enter: a chunked editor rewrite would
	// early-submit and its big paste trips pi's paste guard, so the
	// type verb refuses on editors instead of corrupting the buffer.
	a, sessPath := hcDialogApp(t)
	drv, fake := startHCFake(t, sessPath, editorPaint("probe commit\n"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.statusFor().TargetForPath(context.Background(), sessPath)
		return err == nil
	})
	plantBridge(t, a, sessPath, BridgeDialog{ID: "d9", Kind: "editor", Title: "Commit staged changes with this message?"})

	rec := hcAnswerReq(t, a, `{"id":"d9","answer":"type","text":"feat: x"}`)
	if rec.Code != http.StatusConflict {
		t.Fatalf("code %d body %s, want 409", rec.Code, rec.Body.String())
	}
	if keys := fake.keyLog(); len(keys) != 0 {
		t.Fatalf("keys = %v, want none", keys)
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
		_, err := a.statusFor().TargetForPath(context.Background(), sessPath)
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
		_, err := a.statusFor().TargetForPath(context.Background(), sessPath)
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
