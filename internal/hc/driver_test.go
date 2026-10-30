package hc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"sync"
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
	t.Helper()
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
