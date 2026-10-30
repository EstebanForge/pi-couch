package server

import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
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
	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

// hcStreamFake speaks hc's framed wire for one terminal: a labeled t1
// on events.subscribe, and an attach that paints, streams deltas, and
// holds open until close.
type hcStreamFake struct {
	t    *testing.T
	sock string

	mu     sync.Mutex
	keys   [][]string
	sends  []map[string]any
	paints [][]byte
}

func startHCStreamFake(t *testing.T, sessionPath string) *hcStreamFake {
	t.Helper()
	f := &hcStreamFake{t: t, paints: [][]byte{[]byte("paint-0"), []byte("delta-1")}}
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
			go f.serve(conn, sessionPath)
		}
	}()
	f.sock = sockPath
	return f
}

func (f *hcStreamFake) reply(conn net.Conn, id json.RawMessage, result any) {
	out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "result": result})
	_, _ = conn.Write(append(out, '\n'))
}

func (f *hcStreamFake) record(addr, sessionPath string) map[string]any {
	return map[string]any{
		"address": addr, "agent": "pi", "state": "live", "title": "Pi · " + addr,
		"cwd": "/tmp", "name": "pi-x", "reported": true, "turn": 1,
		"labels": map[string]string{"pi.session": sessionPath},
		"status": map[string]any{"state": "working"},
	}
}

func (f *hcStreamFake) serve(conn net.Conn, sessionPath string) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	for {
		line, err := br.ReadString('\n')
		if err != nil {
			return
		}
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Framed bool `json:"framed"`
			} `json:"params"`
		}
		if json.Unmarshal([]byte(line), &req) != nil {
			return
		}
		switch req.Method {
		case "events.subscribe":
			f.reply(conn, req.ID, map[string]any{"seq": 1, "terminals": []any{f.record("t1", sessionPath)}})
			// Hold the events connection open so the driver stays seeded.
			go func() {
				for {
					if _, err := br.ReadString('\n'); err != nil {
						return
					}
				}
			}()
			return
		case "hello":
			f.reply(conn, req.ID, map[string]any{"generation": 1, "framed": true})
			f.framed(conn, br, req)
			return
		case "terminal.keys":
			var in struct {
				Params struct {
					Keys []string `json:"keys"`
				} `json:"params"`
			}
			_ = json.Unmarshal([]byte(line), &in)
			f.mu.Lock()
			f.keys = append(f.keys, in.Params.Keys)
			f.mu.Unlock()
			f.reply(conn, req.ID, map[string]any{})
		case "terminal.send":
			f.mu.Lock()
			f.sends = append(f.sends, map[string]any{"raw": line})
			f.mu.Unlock()
			f.reply(conn, req.ID, map[string]any{"started": true})
		default:
			f.reply(conn, req.ID, map[string]any{})
		}
	}
}

// framed serves one attach: paint plus deltas, then hold.
func (f *hcStreamFake) framed(conn net.Conn, br *bufio.Reader, hello struct {
	ID     json.RawMessage `json:"id"`
	Method string          `json:"method"`
	Params struct {
		Framed bool `json:"framed"`
	} `json:"params"`
}) {
	for {
		var head [9]byte
		if _, err := readFull(br, head[:]); err != nil {
			return
		}
		channel := binary.LittleEndian.Uint32(head[0:4])
		kind := head[4]
		payload := make([]byte, binary.LittleEndian.Uint32(head[5:9]))
		if _, err := readFull(br, payload); err != nil {
			return
		}
		if channel != 0 || kind != 0 {
			continue
		}
		var call struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
		}
		if json.Unmarshal(payload, &call) != nil || call.Method != "terminal.attach" {
			continue
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": map[string]any{"channel": 9, "terminal": map[string]any{"address": "t1"}}})
		if _, err := conn.Write(encodeFrame(0, 0, out)); err != nil {
			return
		}
		f.mu.Lock()
		paints := f.paints
		f.mu.Unlock()
		for _, p := range paints {
			if _, err := conn.Write(encodeFrame(9, 0, p)); err != nil {
				return
			}
		}
		// Hold the stream open; the test's Close ends it.
		for {
			if _, err := readFull(br, head[:]); err != nil {
				return
			}
			payload = make([]byte, binary.LittleEndian.Uint32(head[5:9]))
			if _, err := readFull(br, payload); err != nil {
				return
			}
		}
	}
}

func encodeFrame(channel uint32, kind byte, payload []byte) []byte {
	out := make([]byte, 9+len(payload))
	binary.LittleEndian.PutUint32(out[0:4], channel)
	out[4] = kind
	binary.LittleEndian.PutUint32(out[5:9], uint32(len(payload)))
	copy(out[9:], payload)
	return out
}

func readFull(br *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := br.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

// hcStreamApp builds a driver=hc app over the stream fake.
func hcStreamApp(t *testing.T) (*app, *hcStreamFake) {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, "sess", "s1.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, path, sessHeader, userLine("a", "", "first"))
	f := startHCStreamFake(t, path)
	drv := hc.NewDriver(hc.NewAt(f.sock))
	drv.SetRetryForTest(20 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { drv.Run(ctx); close(done) }()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("driver did not stop")
		}
	})
	a := &app{
		logger:      testLogger(),
		idx:         pi.NewIndex(root),
		status:      drv,
		hcDrv:       drv,
		bridge:      map[string]bridgeEntry{},
		answered:    newDialogMemos(),
		models:      newModelsCache(t.TempDir()),
		memory:      newMemoryProbe("", ""),
		dialogLatch: map[string]*dialogWire{},
	}
	waitForServer(t, func() bool {
		addr, err := drv.TargetForPath(context.Background(), path)
		return err == nil && addr == "t1"
	})
	return a, f
}

func waitForServer(t *testing.T, cond func() bool) {
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

func TestHCTerminalStreamsDeltas(t *testing.T) {
	a, _ := hcStreamApp(t)
	ctx, cancel := context.WithCancel(context.Background())
	req := httptest.NewRequest(http.MethodGet, "/stream/terminal/s1?cols=100&rows=30", nil).WithContext(ctx)
	req.SetPathValue("key", "s1")
	w := httptest.NewRecorder()
	done := make(chan struct{})
	go func() { a.streamTerminal(w, req); close(done) }()
	waitForServer(t, func() bool {
		return strings.Contains(w.Body.String(), base64.StdEncoding.EncodeToString([]byte("delta-1")))
	})
	cancel()
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("stream never ended after cancel")
	}
	body := w.Body.String()
	if !strings.Contains(body, "event: delta") {
		t.Fatalf("no delta events in %q", body)
	}
	want0 := base64.StdEncoding.EncodeToString([]byte("paint-0"))
	want1 := base64.StdEncoding.EncodeToString([]byte("delta-1"))
	if !strings.Contains(body, want0) || !strings.Contains(body, want1) {
		t.Fatalf("paint and delta missing from %q", body)
	}
}

func TestHCTerminalInputSendsKeysAndRefusesRawText(t *testing.T) {
	a, f := hcStreamApp(t)
	post := func(body string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/terminal/input", strings.NewReader(body))
		req.SetPathValue("key", "s1")
		w := httptest.NewRecorder()
		a.terminalInput(w, req)
		return w
	}
	if w := post(`{"keys":["up","enter"]}`); w.Code != http.StatusNoContent {
		t.Fatalf("keys: code=%d body=%s", w.Code, w.Body.String())
	}
	waitForServer(t, func() bool { return len(f.keyLog()) == 1 })
	if got := f.keyLog()[0]; len(got) != 2 || got[0] != "up" || got[1] != "enter" {
		t.Fatalf("keys = %v, want up+enter", got)
	}
	if w := post(`{"text":"raw typing"}`); w.Code != http.StatusConflict {
		t.Fatalf("raw text under hc: code=%d, want 409 (Send always submits)", w.Code)
	}
}

func (f *hcStreamFake) sendParams() []map[string]any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]map[string]any(nil), f.sends...)
}

func (f *hcStreamFake) keyLog() [][]string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([][]string(nil), f.keys...)
}

// Regression for the main.go wiring bug: even if a herdr client is
// non-nil under hc, commands must route to the hc driver, never to
// the herdr socket with an hc address.
func TestHCCommandsNeverDialHerdr(t *testing.T) {
	a, f := hcStreamApp(t)
	a.herdr = herdr.New() // live default socket; a dial here fails the test
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/prompt", strings.NewReader(`{"text":"hi"}`))
	req.SetPathValue("key", "s1")
	w := httptest.NewRecorder()
	a.apiPrompt(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("prompt: code=%d body=%s", w.Code, w.Body.String())
	}
	waitForServer(t, func() bool { return len(f.sendParams()) == 1 })
}
