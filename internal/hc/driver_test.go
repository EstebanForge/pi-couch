package hc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// record builds a public terminal record as the core would list it.
func record(addr, path, state, statusState string) map[string]any {
	labels := map[string]any{}
	if path != "" {
		labels["pi.session"] = path
	}
	var status any
	if statusState != "" {
		status = map[string]any{"state": statusState, "app": "pi"}
	}
	return map[string]any{
		"address":  addr,
		"labels":   labels,
		"state":    state,
		"status":   status,
		"reported": status != nil,
		"title":    "Pi · " + addr,
	}
}

// eventLine builds one notification line as the core writes it.
func eventLine(kind, addr string, data map[string]any) []byte {
	b, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  "event",
		"params":  map[string]any{"type": kind, "terminal": addr, "data": data, "seq": 1},
	})
	return append(b, '\n')
}

// startDriver runs a Driver against a fake core whose events.subscribe
// reply carries snap; the test receives the held events connection to
// push notifications through.
func startDriver(t *testing.T, snap []map[string]any) (*Driver, *fakeCore, <-chan net.Conn) {
	return startDriverReply(t, func(method string, _ json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": snap}, nil
		}
		return map[string]any{}, nil
	})
}

// startDriverReply is the handler-injectable harness: command methods
// (terminal.send, terminal.interrupt, ...) reply from the handler.
func startDriverReply(t *testing.T, reply func(method string, params json.RawMessage) (any, *Error)) (*Driver, *fakeCore, <-chan net.Conn) {
	t.Helper()
	mu := sync.Mutex{}
	conns := make(chan net.Conn, 4)
	f := startFakeCore(t, func(method string, params json.RawMessage) (any, *Error) {
		mu.Lock()
		defer mu.Unlock()
		return reply(method, params)
	})
	f.afterReply = func(conn net.Conn, method string) bool {
		if method == "events.subscribe" {
			conns <- conn
			return true
		}
		return false
	}
	d := NewDriver(NewAt(f.socket()))
	d.retry = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		d.Run(ctx)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("driver did not stop")
		}
	})
	return d, f, conns
}

func waitFor(t *testing.T, cond func() bool) {
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

func TestDriverTracksSessionLabels(t *testing.T) {
	snap := []map[string]any{
		record("t1", "/home/me/.pi/sessions/a.jsonl", "live", "working"),
		record("t2", "", "live", "done"),
	}
	d, _, conns := startDriver(t, snap)

	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return err == nil
	})
	badges, driven := d.DriveStatus(context.Background())
	if badges["/home/me/.pi/sessions/a.jsonl"] != "working" {
		t.Fatalf("badge = %v, want working for a.jsonl", badges)
	}
	if len(badges) != 1 {
		t.Fatalf("badges = %v, want only the labeled session", badges)
	}
	if driven["/home/me/.pi/sessions/a.jsonl"].Title != "Pi · t1" {
		t.Fatalf("driven title = %q, want the terminal title", driven["/home/me/.pi/sessions/a.jsonl"].Title)
	}
	addr, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
	if err != nil || addr != "t1" {
		t.Fatalf("target = %q, %v, want t1", addr, err)
	}
	if _, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/none.jsonl"); !errors.Is(err, herdr.ErrNotDriven) {
		t.Fatalf("unknown session err = %v, want ErrNotDriven", err)
	}

	conn := <-conns
	// t2 gains its pi.session label late (the bridge labels at session
	// start) and reports working.
	if _, err := conn.Write(eventLine("labels", "t2", map[string]any{
		"labels": map[string]any{"pi.session": "/home/me/.pi/sessions/b.jsonl"},
	})); err != nil {
		t.Fatal(err)
	}
	if _, err := conn.Write(eventLine("status", "t2", map[string]any{
		"status": map[string]any{"state": "working", "app": "pi"},
	})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		addr, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/b.jsonl")
		return err == nil && addr == "t2"
	})
	if badges, _ := d.DriveStatus(context.Background()); badges["/home/me/.pi/sessions/b.jsonl"] != "working" {
		t.Fatalf("badge after status event = %v, want working", badges)
	}

	// t1 finishes: the session is no longer driven.
	if _, err := conn.Write(eventLine("finished", "t1", map[string]any{})); err != nil {
		t.Fatal(err)
	}
	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return errors.Is(err, herdr.ErrNotDriven)
	})
	if badges, _ := d.DriveStatus(context.Background()); len(badges) != 1 {
		t.Fatalf("badges after finish = %v, want only b.jsonl", badges)
	}
}

func TestDriverResubscribesAfterStreamLoss(t *testing.T) {
	snap := []map[string]any{
		record("t1", "/home/me/.pi/sessions/a.jsonl", "live", "working"),
	}
	mu := sync.Mutex{}
	handler := func(method string, _ json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			mu.Lock()
			defer mu.Unlock()
			return map[string]any{"seq": 1, "terminals": snap}, nil
		}
		return map[string]any{}, nil
	}
	conns := make(chan net.Conn, 4)
	f := startFakeCore(t, handler)
	f.afterReply = func(conn net.Conn, method string) bool {
		if method == "events.subscribe" {
			conns <- conn
			return true
		}
		return false
	}
	d := NewDriver(NewAt(f.socket()))
	d.retry = 20 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go d.Run(ctx)

	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return err == nil
	})

	// The core dies under us and comes back with the terminal moved.
	(<-conns).Close()
	mu.Lock()
	snap = []map[string]any{
		record("t1", "/home/me/.pi/sessions/moved.jsonl", "live", "working"),
	}
	mu.Unlock()
	waitFor(t, func() bool {
		addr, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/moved.jsonl")
		return err == nil && addr == "t1"
	})
	if _, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl"); !errors.Is(err, herdr.ErrNotDriven) {
		t.Fatalf("stale path still resolved: %v", err)
	}
	if n := len(f.seen("events.subscribe")); n < 2 {
		t.Fatalf("subscribe calls = %d, want at least 2", n)
	}
}

func TestDriverPromptSendsToTheTerminal(t *testing.T) {
	var mu sync.Mutex
	var sent []map[string]any
	d, f, _ := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": []map[string]any{
				record("t1", "/home/me/.pi/sessions/a.jsonl", "live", "idle"),
			}}, nil
		}
		if method == "terminal.send" {
			var p map[string]any
			_ = json.Unmarshal(params, &p)
			mu.Lock()
			sent = append(sent, p)
			mu.Unlock()
			return map[string]any{"started": true, "turn": 1}, nil
		}
		return map[string]any{}, nil
	})
	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return err == nil
	})

	if err := d.Prompt(context.Background(), "t1", "hi"); err != nil {
		t.Fatalf("prompt: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 {
		t.Fatalf("sends = %d, want 1", len(sent))
	}
	if sent[0]["terminal"] != "t1" || sent[0]["text"] != "hi" {
		t.Fatalf("send params = %v, want terminal t1 text hi", sent[0])
	}
	if _, ok := sent[0]["caller"]; ok {
		t.Fatal("caller must never ride a send")
	}
	if n := len(f.seen("terminal.send")); n != 1 {
		t.Fatalf("send calls = %d, want 1", n)
	}
}

func TestDriverPromptRetriesWhenFirstPaintEatsTheEnter(t *testing.T) {
	var sends int32
	d, f, _ := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": []map[string]any{
				record("t1", "/home/me/.pi/sessions/a.jsonl", "live", ""),
			}}, nil
		}
		if method == "terminal.send" {
			if atomic.AddInt32(&sends, 1) == 1 {
				return map[string]any{"started": false, "turn": 0}, nil
			}
			return map[string]any{"started": true, "turn": 0}, nil
		}
		return map[string]any{}, nil
	})
	d.reportWait = 50 * time.Millisecond
	d.sendRetry = 20 * time.Millisecond
	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return err == nil
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := d.Prompt(ctx, "t1", "hi"); err != nil {
		t.Fatalf("prompt with retry: %v", err)
	}
	if n := len(f.seen("terminal.send")); n != 2 {
		t.Fatalf("send calls = %d, want 2 (one retry)", n)
	}
}

func TestDriverStopInterrupts(t *testing.T) {
	d, f, _ := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": []map[string]any{
				record("t1", "/home/me/.pi/sessions/a.jsonl", "live", "working"),
			}}, nil
		}
		if method == "terminal.interrupt" {
			return map[string]any{"outcome": "stopped", "key": "esc"}, nil
		}
		return map[string]any{}, nil
	})
	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return err == nil
	})

	if err := d.Stop(context.Background(), "t1"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	if n := len(f.seen("terminal.interrupt")); n != 1 {
		t.Fatalf("interrupt calls = %d, want 1", n)
	}
}

func TestDriverCloseIsGracefulThenEscalates(t *testing.T) {
	// pi ignores the first ctrl+d in this core: close must escalate to
	// kill, then rm the record.
	d, f, _ := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": []map[string]any{
				record("t1", "/home/me/.pi/sessions/a.jsonl", "live", "idle"),
			}}, nil
		}
		return map[string]any{}, nil
	})
	d.finishWait = 50 * time.Millisecond
	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return err == nil
	})

	if err := d.Close(context.Background(), "t1"); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n := len(f.seen("terminal.keys")); n != 1 {
		t.Fatalf("keys calls = %d, want 1 (ctrl+d)", n)
	}
	if n := len(f.seen("terminal.kill")); n != 1 {
		t.Fatalf("kill calls = %d, want 1 (escalation)", n)
	}
	if n := len(f.seen("terminal.rm")); n != 1 {
		t.Fatalf("rm calls = %d, want 1", n)
	}
}

func TestDriverCloseSkipsTheKillWhenPiExits(t *testing.T) {
	var d *Driver
	d, f, _ := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": []map[string]any{
				record("t1", "/home/me/.pi/sessions/a.jsonl", "live", "idle"),
			}}, nil
		}
		if method == "terminal.keys" {
			// pi took the ctrl+d: the record is gone by the next look.
			d.mu.Lock()
			delete(d.byAddr, "t1")
			delete(d.byPath, "/home/me/.pi/sessions/a.jsonl")
			d.mu.Unlock()
			return map[string]any{}, nil
		}
		return map[string]any{}, nil
	})
	d.finishWait = 50 * time.Millisecond
	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return err == nil
	})

	if err := d.Close(context.Background(), "t1"); err != nil {
		t.Fatalf("close: %v", err)
	}
	if n := len(f.seen("terminal.kill")); n != 0 {
		t.Fatalf("kill calls = %d, want 0 (graceful exit)", n)
	}
	if n := len(f.seen("terminal.rm")); n != 1 {
		t.Fatalf("rm calls = %d, want 1", n)
	}
}
