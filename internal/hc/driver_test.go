package hc

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"strings"
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

func TestDriverPromptNudgesEnterWhenFirstPaintEatsIt(t *testing.T) {
	d, f, _ := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": []map[string]any{
				record("t1", "/home/me/.pi/sessions/a.jsonl", "live", ""),
			}}, nil
		}
		if method == "terminal.send" {
			// The paste landed but the Enter was lost: the draft is
			// sitting in the composer, so a resend would double it.
			return map[string]any{"started": false, "turn": 0}, nil
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
		t.Fatalf("prompt with enter nudge: %v", err)
	}
	if n := len(f.seen("terminal.send")); n != 1 {
		t.Fatalf("send calls = %d, want exactly 1 (no doubled prompt)", n)
	}
	if n := len(f.seen("terminal.keys")); n != 1 {
		t.Fatalf("keys calls = %d, want 1 enter nudge", n)
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

func TestDriverServesThePaneSource(t *testing.T) {
	d, _, _ := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": []map[string]any{
				record("t1", "/home/me/.pi/sessions/a.jsonl", "live", "working"),
				record("t2", "", "live", "done"),
			}}, nil
		}
		if method == "terminal.read" {
			return map[string]any{"text": "screen text\n", "complete": true, "how": "screen"}, nil
		}
		return map[string]any{}, nil
	})
	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl")
		return err == nil
	})

	agents, err := d.Agents(context.Background())
	if err != nil {
		t.Fatalf("agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("agents = %d, want the one labeled session", len(agents))
	}
	a := agents[0]
	if a.Session == nil || a.Session.Value != "/home/me/.pi/sessions/a.jsonl" {
		t.Fatalf("session = %+v, want a.jsonl", a.Session)
	}
	if a.PaneID != "t1" {
		t.Fatalf("pane = %q, want t1", a.PaneID)
	}
	if a.AgentStatus != "working" {
		t.Fatalf("status = %q, want working", a.AgentStatus)
	}

	pr, err := d.PaneRead(context.Background(), "t1", "detection", 30)
	if err != nil {
		t.Fatalf("pane read: %v", err)
	}
	if pr.PaneID != "t1" || pr.Text != "screen text\n" {
		t.Fatalf("read = %q / %q, want t1 / screen text", pr.PaneID, pr.Text)
	}
}

func TestDriverSpawnRunsPiAndWaitsForTheFirstReport(t *testing.T) {
	var mu sync.Mutex
	var runs []map[string]any
	d, _, conns := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		switch method {
		case "events.subscribe":
			return map[string]any{"seq": 1, "terminals": []map[string]any{}}, nil
		case "terminal.run":
			var p map[string]any
			_ = json.Unmarshal(params, &p)
			mu.Lock()
			runs = append(runs, p)
			mu.Unlock()
			return map[string]any{"address": "t9", "name": p["name"], "state": "live"}, nil
		}
		return map[string]any{}, nil
	})
	d.spawnReadyWait = 2 * time.Second

	type spawnRes struct {
		addr string
		err  error
	}
	done := make(chan spawnRes, 1)
	go func() {
		t9, err := d.Spawn(context.Background(), "/home/me/proj", "pi-demo", []string{"--model", "x/y"})
		done <- spawnRes{t9.Address, err}
	}()

	var conn net.Conn
	select {
	case conn = <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("no held subscribe conn")
	}
	waitFor(t, func() bool { mu.Lock(); defer mu.Unlock(); return len(runs) == 1 })
	if _, err := conn.Write(eventLine("created", "t9", map[string]any{
		"address": "t9", "name": "pi-demo", "state": "live",
	})); err != nil {
		t.Fatal(err)
	}
	time.Sleep(50 * time.Millisecond)
	if _, err := conn.Write(eventLine("status", "t9", map[string]any{
		"status":   map[string]any{"state": "working", "app": "pi"},
		"reported": true,
	})); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-done:
		if r.err != nil {
			t.Fatalf("spawn: %v", r.err)
		}
		if r.addr != "t9" {
			t.Fatalf("spawn addr = %q, want t9", r.addr)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("spawn never returned")
	}

	mu.Lock()
	defer mu.Unlock()
	line, _ := runs[0]["line"].(string)
	if !strings.HasPrefix(line, "pi ") || !strings.Contains(line, "--model") || !strings.Contains(line, "x/y") {
		t.Fatalf("run line = %q, want a quoted pi command", line)
	}
	if runs[0]["cwd"] != "/home/me/proj" || runs[0]["name"] != "pi-demo" {
		t.Fatalf("run params = %v, want the cwd and name", runs[0])
	}
	if _, ok := runs[0]["caller"]; ok {
		t.Fatal("caller must never ride a run")
	}
}

func TestDriverSpawnTimesOutWhenPiNeverReports(t *testing.T) {
	d, f, conns := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		if method == "events.subscribe" {
			return map[string]any{"seq": 1, "terminals": []map[string]any{}}, nil
		}
		if method == "terminal.run" {
			return map[string]any{"address": "t9", "state": "live"}, nil
		}
		return map[string]any{}, nil
	})
	d.spawnReadyWait = 60 * time.Millisecond
	select {
	case <-conns:
	case <-time.After(2 * time.Second):
		t.Fatal("no held subscribe conn")
	}
	rec, err := d.Spawn(context.Background(), "/home/me", "pi-x", nil)
	if err == nil {
		t.Fatal("spawn without a first report must fail")
	}
	if rec.Address != "" {
		t.Fatalf("failed spawn returned live record %+v: callers must not have to clean up", rec)
	}
	// The failed spawn must not leak a live pi for the retry to shadow.
	if len(f.seen("terminal.kill")) != 1 || len(f.seen("terminal.rm")) != 1 {
		t.Fatalf("kill=%d rm=%d, want the failed spawn discarded", len(f.seen("terminal.kill")), len(f.seen("terminal.rm")))
	}
}

func TestDriverSessionLabel(t *testing.T) {
	d, _, _ := startDriver(t, []map[string]any{
		record("t1", "/x/a.jsonl", "live", "idle"),
	})
	waitFor(t, func() bool {
		_, ok := d.SessionLabel("t1")
		return ok
	})
	if p, ok := d.SessionLabel("t1"); !ok || p != "/x/a.jsonl" {
		t.Fatalf("label = %q, %v, want /x/a.jsonl", p, ok)
	}
	if _, ok := d.SessionLabel("t0"); ok {
		t.Fatal("unknown terminal must not carry a label")
	}
}

func TestDriverDiscardKillsThenRemoves(t *testing.T) {
	var mu sync.Mutex
	var order []string
	d, _, _ := startDriverReply(t, func(method string, params json.RawMessage) (any, *Error) {
		switch method {
		case "events.subscribe":
			return map[string]any{"seq": 1, "terminals": []map[string]any{
				record("t1", "/x/a.jsonl", "live", "idle"),
			}}, nil
		case "terminal.kill", "terminal.rm":
			mu.Lock()
			order = append(order, method)
			mu.Unlock()
			return map[string]any{}, nil
		}
		return map[string]any{}, nil
	})
	waitFor(t, func() bool {
		_, err := d.TargetForPath(context.Background(), "/x/a.jsonl")
		return err == nil
	})
	if err := d.Discard(context.Background(), "t1"); err != nil {
		t.Fatalf("discard: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(order) != 2 || order[0] != "terminal.kill" || order[1] != "terminal.rm" {
		t.Fatalf("discard calls = %v, want kill then rm", order)
	}
}

// A subscriber that falls behind gets `missed` plus a fresh snapshot,
// not a backlog. Treat it as a stream loss: resubscribe and reseed.
func TestDriverReseedsOnMissed(t *testing.T) {
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

	// The core drops our backlog and offers a fresh snapshot instead.
	mu.Lock()
	snap = []map[string]any{
		record("t1", "/home/me/.pi/sessions/moved.jsonl", "live", "working"),
	}
	mu.Unlock()
	conn := <-conns
	_, _ = conn.Write([]byte(`{"jsonrpc":"2.0","method":"missed","params":{"seq":9,"terminals":[]}}` + "\n"))

	waitFor(t, func() bool {
		addr, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/moved.jsonl")
		return err == nil && addr == "t1"
	})
	if _, err := d.TargetForPath(context.Background(), "/home/me/.pi/sessions/a.jsonl"); err == nil {
		t.Fatal("stale path must drop after a missed resync")
	}
}
