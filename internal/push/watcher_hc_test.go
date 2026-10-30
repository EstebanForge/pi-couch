package push

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
)

// hcWatchFake serves the slice of hc's JSON-RPC the watcher needs: an
// events.subscribe reply (snapshot from the test), a held connection
// per subscribe the test pushes notification lines through, and
// terminal.get resolving addresses to labeled paths.
type hcWatchFake struct {
	t    *testing.T
	sock string

	snapshot func() []map[string]any
	paths    map[string]string // terminal address -> pi.session label

	mu   sync.Mutex
	subs []net.Conn // connections that subscribed, in order
}

func startHCWatchFake(t *testing.T, paths map[string]string, snapshot func() []map[string]any) *hcWatchFake {
	t.Helper()
	if snapshot == nil {
		snapshot = func() []map[string]any { return nil }
	}
	f := &hcWatchFake{t: t, paths: paths, snapshot: snapshot}
	sockPath := filepath.Join(t.TempDir(), "hc.sock")
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
	return f
}

func (f *hcWatchFake) serve(conn net.Conn) {
	defer conn.Close()
	defer f.t.Logf("hc fake: serve exited")
	sc := bufio.NewScanner(conn)
	sc.Buffer(make([]byte, 0, 4096), 1<<20)
	for sc.Scan() {
		var req struct {
			ID     json.RawMessage `json:"id"`
			Method string          `json:"method"`
			Params struct {
				Terminal string `json:"terminal"`
			} `json:"params"`
		}
		if err := json.Unmarshal(sc.Bytes(), &req); err != nil {
			continue
		}
		var result any
		switch req.Method {
		case "events.subscribe":
			f.mu.Lock()
			f.subs = append(f.subs, conn)
			f.mu.Unlock()
			result = map[string]any{"seq": 1, "terminals": f.snapshot()}
		case "terminal.get":
			path := f.paths[req.Params.Terminal]
			rec := map[string]any{"address": req.Params.Terminal, "state": "live"}
			if path != "" {
				rec["labels"] = map[string]any{"pi.session": path}
				rec["title"] = "Pi · " + req.Params.Terminal
			}
			result = rec
		default:
			result = map[string]any{}
		}
		out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": result})
		if _, err := conn.Write(append(out, '\n')); err != nil {
			f.t.Logf("hc fake: write reply: %v", err)
			return
		}
	}
	if err := sc.Err(); err != nil {
		f.t.Logf("hc fake: scanner err: %v", err)
	}
}

// notify writes one event notification to the newest subscribe
// connection (call conns close after their reply; only subscriptions
// stay).
func (f *hcWatchFake) notify(kind, addr string, data map[string]any) {
	deadline := time.Now().Add(2 * time.Second)
	for {
		f.mu.Lock()
		n := len(f.subs)
		var conn net.Conn
		if n > 0 {
			conn = f.subs[n-1]
		}
		f.mu.Unlock()
		if conn != nil {
			f.write(conn, kind, addr, data)
			return
		}
		if time.Now().After(deadline) {
			f.t.Fatal("no held connection to notify on")
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func (f *hcWatchFake) write(conn net.Conn, kind, addr string, data map[string]any) {
	line, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params":  map[string]any{"type": kind, "terminal": addr, "data": data, "seq": 1},
	})
	if _, err := conn.Write(append(line, '\n')); err != nil {
		f.t.Fatalf("notify: %v", err)
	}
}

func (f *hcWatchFake) dropConns() {
	f.mu.Lock()
	subs := f.subs
	f.subs = nil
	f.mu.Unlock()
	for _, c := range subs {
		_ = c.Close()
	}
}

func waitForPush(t *testing.T, pushes *[]Event, want int) *[]Event {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if len(*pushes) >= want {
			return pushes
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("pushes = %d, want %d", len(*pushes), want)
	return pushes
}

func startHCWatcher(t *testing.T, f *hcWatchFake) *[]Event {
	t.Helper()
	pushes := &[]Event{}
	w := NewHCWatcher(hc.NewAt(f.sock))
	w.Retry = 20 * time.Millisecond
	w.OnEvent = func(e Event) {
		*pushes = append(*pushes, e)
	}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.Run(ctx)
	return pushes
}

func recordSnapshot(addr, path, state string) map[string]any {
	rec := map[string]any{"address": addr, "state": "live"}
	if path != "" {
		rec["labels"] = map[string]any{"pi.session": path}
	}
	if state != "" {
		rec["status"] = map[string]any{"state": state, "app": "pi"}
	}
	return rec
}

func TestHCTurnDonePushesOncePerTurn(t *testing.T) {
	f := startHCWatchFake(t, map[string]string{"t1": "/x/a.jsonl"}, nil)
	pushes := startHCWatcher(t, f)

	f.notify("turn", "t1", map[string]any{"turn": 1, "state": "done"})
	pushes = waitForPush(t, pushes, 1)
	if (*pushes)[0].Status != "done" || (*pushes)[0].Path != "/x/a.jsonl" {
		t.Fatalf("push = %+v, want done for a.jsonl", (*pushes)[0])
	}

	// The same turn again (a replay) must not re-buzz.
	f.notify("turn", "t1", map[string]any{"turn": 1, "state": "done"})
	time.Sleep(100 * time.Millisecond)
	if len(*pushes) != 1 {
		t.Fatalf("pushes = %d, want 1 after replay", len(*pushes))
	}

	// The next turn's done pushes again.
	f.notify("turn", "t1", map[string]any{"turn": 2, "state": "working"})
	f.notify("turn", "t1", map[string]any{"turn": 2, "state": "done"})
	waitForPush(t, pushes, 2)
	if (*pushes)[1].Status != "done" {
		t.Fatalf("second push = %+v, want done", (*pushes)[1])
	}
}

func TestHCBlockedPushesAndRearms(t *testing.T) {
	f := startHCWatchFake(t, map[string]string{"t1": "/x/a.jsonl"}, nil)
	pushes := startHCWatcher(t, f)

	f.notify("status", "t1", map[string]any{"status": map[string]any{"state": "blocked", "app": "pi"}})
	pushes = waitForPush(t, pushes, 1)
	if (*pushes)[0].Status != "blocked" {
		t.Fatalf("push = %+v, want blocked", (*pushes)[0])
	}

	// Blocked holding still: no re-buzz.
	f.notify("status", "t1", map[string]any{"status": map[string]any{"state": "blocked", "app": "pi"}})
	time.Sleep(100 * time.Millisecond)
	if len(*pushes) != 1 {
		t.Fatalf("pushes = %d, want 1 while blocked holds", len(*pushes))
	}

	// Working then blocked again: the phone buzzes for the new park.
	f.notify("status", "t1", map[string]any{"status": map[string]any{"state": "working", "app": "pi"}})
	f.notify("status", "t1", map[string]any{"status": map[string]any{"state": "blocked", "app": "pi"}})
	waitForPush(t, pushes, 2)
}

func TestHCErrorStatusPushes(t *testing.T) {
	f := startHCWatchFake(t, map[string]string{"t1": "/x/a.jsonl"}, nil)
	pushes := startHCWatcher(t, f)

	f.notify("status", "t1", map[string]any{"status": map[string]any{"state": "error", "app": "pi"}})
	pushes = waitForPush(t, pushes, 1)
	if (*pushes)[0].Status != "error" {
		t.Fatalf("push = %+v, want error", (*pushes)[0])
	}
}

func TestHCUnlabeledTerminalsNeverPush(t *testing.T) {
	f := startHCWatchFake(t, map[string]string{}, nil)
	pushes := startHCWatcher(t, f)

	f.notify("turn", "t9", map[string]any{"turn": 1, "state": "done"})
	f.notify("status", "t9", map[string]any{"status": map[string]any{"state": "blocked", "app": "pi"}})
	time.Sleep(150 * time.Millisecond)
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %+v, want none for an unlabeled terminal", *pushes)
	}
}

func TestHCReconnectPrimesFromSnapshot(t *testing.T) {
	snap := []map[string]any{recordSnapshot("t1", "/x/a.jsonl", "done")}
	f := startHCWatchFake(t, map[string]string{"t1": "/x/a.jsonl"}, func() []map[string]any { return snap })
	pushes := startHCWatcher(t, f)

	// The done in the very first snapshot is old news: no buzz.
	time.Sleep(150 * time.Millisecond)
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %+v, want none from the seed snapshot", *pushes)
	}

	// A stream loss reconnects and re-seeds the same done: still quiet.
	// dropConns zeroed the held set, so one fresh subscribe is the
	// reconnect proof.
	f.dropConns()
	waitForReconnect(t, f, 1)
	time.Sleep(150 * time.Millisecond)
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %+v, want none after the primed reconnect", *pushes)
	}
}

func waitForReconnect(t *testing.T, f *hcWatchFake, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		f.mu.Lock()
		n := len(f.subs)
		f.mu.Unlock()
		if n >= want {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	f.mu.Lock()
	n := len(f.subs)
	f.mu.Unlock()
	t.Fatalf("subscribe conns = %d, want %d", n, want)
}

// Production wires the watcher as a struct literal (main.go), not the
// constructor. Run must still be safe: no nil-map panic, no dial spin.
func TestHCWatcherStructLiteralIsSafe(t *testing.T) {
	f := startHCWatchFake(t, map[string]string{"t1": "/x/s1.jsonl"}, func() []map[string]any {
		return []map[string]any{recordSnapshot("t1", "/x/s1.jsonl", "working")}
	})
	pushes := &[]Event{}
	w := &HCWatcher{Client: hc.NewAt(f.sock), OnEvent: func(e Event) { *pushes = append(*pushes, e) }}
	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	go w.Run(ctx)
	f.notify("turn", "t1", map[string]any{"turn": 1, "state": "done"})
	waitForPush(t, pushes, 1)
}

func TestHCMissedResyncsWithoutBuzzing(t *testing.T) {
	snap := []map[string]any{recordSnapshot("t1", "/x/a.jsonl", "done")}
	f := startHCWatchFake(t, map[string]string{"t1": "/x/a.jsonl"}, func() []map[string]any { return snap })
	pushes := startHCWatcher(t, f)
	time.Sleep(150 * time.Millisecond)
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %+v, want none from the seed snapshot", *pushes)
	}

	// A dropped backlog arrives as missed: the watcher must resubscribe
	// and re-prime from the fresh snapshot, not replay the done.
	f.mu.Lock()
	subs := f.subs
	f.subs = nil
	f.mu.Unlock()
	for _, c := range subs {
		_, _ = c.Write([]byte(`{"jsonrpc":"2.0","method":"missed","params":{"seq":9,"terminals":[]}}` + "\n"))
	}
	waitForReconnect(t, f, 1)
	time.Sleep(150 * time.Millisecond)
	if len(*pushes) != 0 {
		t.Fatalf("pushes = %+v, want none after a missed resync", *pushes)
	}
}
