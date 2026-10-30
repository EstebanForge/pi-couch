package hc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestRunDecodesTheTerminalRecord(t *testing.T) {
	var gotParams map[string]any
	fake := startFakeCore(t, func(method string, params json.RawMessage) (any, *Error) {
		if method != "terminal.run" {
			t.Fatalf("method %q", method)
		}
		if err := json.Unmarshal(params, &gotParams); err != nil {
			t.Fatal(err)
		}
		return map[string]any{
			"address": "t7", "name": "probe", "argv": []string{"pi"},
			"cwd": "/tmp/x", "state": "live", "cols": 200, "rows": 120,
			"created_ms": 1234, "labels": map[string]string{"probe": "1"},
			"status": map[string]any{"state": "idle", "app": "pi"},
			"turn":   0, "reported": true,
		}, nil
	})
	c := NewAt(fake.socket())
	term, err := c.Run(context.Background(), RunParams{
		Argv:   []string{"pi"},
		Name:   "probe",
		Cwd:    "/tmp/x",
		Labels: map[string]string{"probe": "1"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if term.Address != "t7" || term.State != "live" || term.Cols != 200 || term.Rows != 120 {
		t.Fatalf("terminal %+v", term)
	}
	if term.Status == nil || term.Status.State != "idle" || term.Status.App != "pi" {
		t.Fatalf("status %+v", term.Status)
	}
	if !term.Reported || term.Turn != 0 {
		t.Fatalf("terminal %+v", term)
	}
	if gotParams["name"] != "probe" || gotParams["cwd"] != "/tmp/x" {
		t.Fatalf("params %v", gotParams)
	}
	labels, ok := gotParams["labels"].(map[string]any)
	if !ok || labels["probe"] != "1" {
		t.Fatalf("labels %v", gotParams["labels"])
	}
}

func TestListFiltersByLabels(t *testing.T) {
	fake := startFakeCore(t, func(method string, params json.RawMessage) (any, *Error) {
		return map[string]any{
			"terminals": []any{map[string]any{
				"address": "t2", "state": "live",
				"labels": map[string]string{"pi.session": "/home/e/.pi/agent/sessions/a.jsonl"},
				"status": map[string]any{"state": "working", "app": "pi", "msg": "fixing"},
			}},
			"hidden_finished": 3,
		}, nil
	})
	c := NewAt(fake.socket())
	terms, err := c.List(context.Background(), ListParams{Labels: map[string]string{"pi.session": "/home/e/.pi/agent/sessions/a.jsonl"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(terms) != 1 || terms[0].Address != "t2" {
		t.Fatalf("terms %+v", terms)
	}
	if terms[0].Status == nil || terms[0].Status.State != "working" || terms[0].Status.Msg != "fixing" {
		t.Fatalf("status %+v", terms[0].Status)
	}
	if terms[0].Labels["pi.session"] != "/home/e/.pi/agent/sessions/a.jsonl" {
		t.Fatalf("labels %v", terms[0].Labels)
	}
	params := fake.seen("terminal.list")[0]
	if _, has := params["all"]; has {
		t.Fatalf("all=false must be omitted, params %v", params)
	}
	lbls := params["labels"].(map[string]any)
	if lbls["pi.session"] != "/home/e/.pi/agent/sessions/a.jsonl" {
		t.Fatalf("label filter %v", lbls)
	}
	if _, err := c.List(context.Background(), ListParams{All: true}); err != nil {
		t.Fatal(err)
	}
	if fake.seen("terminal.list")[1]["all"] != true {
		t.Fatal("all=true must ride the request")
	}
}

func TestSendCarriesWaitAndDecodesTheResult(t *testing.T) {
	fake := startFakeCore(t, func(method string, params json.RawMessage) (any, *Error) {
		return map[string]any{
			"started": true, "status": "working", "turn": 3,
			"wait": map[string]any{
				"terminal": "t2", "ended": true, "reason": "status",
				"status": "done", "turn": 4,
			},
		}, nil
	})
	c := NewAt(fake.socket())
	res, err := c.Send(context.Background(), "t2", "Reply with: pong", SendParams{Wait: true, Timeout: 120 * time.Second})
	if err != nil {
		t.Fatal(err)
	}
	if res.Started == nil || !*res.Started || res.Status == nil || *res.Status != "working" {
		t.Fatalf("send result %+v", res)
	}
	if res.Wait == nil || res.Wait.Reason != "status" || res.Wait.Status == nil || *res.Wait.Status != "done" || res.Wait.Turn != 4 {
		t.Fatalf("wait %+v", res.Wait)
	}
	params := fake.seen("terminal.send")[0]
	if params["text"] != "Reply with: pong" || params["wait"] != true {
		t.Fatalf("params %v", params)
	}
	if params["timeout"] != 120.0 {
		t.Fatalf("timeout %v", params["timeout"])
	}
	if _, has := params["caller"]; has {
		t.Fatal("caller must never be sent: hc would wrap text in <hc from=...> tags")
	}
}

func TestKeysPutAndLabelShape(t *testing.T) {
	fake := startFakeCore(t, func(method string, params json.RawMessage) (any, *Error) {
		return map[string]any{"status": "idle", "turn": 1}, nil
	})
	c := NewAt(fake.socket())
	if err := c.Keys(context.Background(), "t2", []string{"enter"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Label(context.Background(), "t2", map[string]string{"pi.session": "/tmp/a.jsonl"}); err != nil {
		t.Fatal(err)
	}
	keys := fake.seen("terminal.keys")[0]
	if got := keys["keys"].([]any); len(got) != 1 || got[0] != "enter" {
		t.Fatalf("keys %v", keys)
	}
	lbls := fake.seen("terminal.label")[0]["labels"].(map[string]any)
	if lbls["pi.session"] != "/tmp/a.jsonl" {
		t.Fatalf("labels %v", lbls)
	}
}

func TestReadDecodesScreenText(t *testing.T) {
	fake := startFakeCore(t, func(method string, params json.RawMessage) (any, *Error) {
		return map[string]any{"text": "Allow edit?\n[1] Yes", "complete": true, "how": "screen"}, nil
	})
	c := NewAt(fake.socket())
	res, err := c.Read(context.Background(), "t2", 30)
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "Allow edit?\n[1] Yes" || !res.Complete || res.How != "screen" {
		t.Fatalf("read %+v", res)
	}
	params := fake.seen("terminal.read")[0]
	if params["lines"] != 30.0 || params["terminal"] != "t2" {
		t.Fatalf("params %v", params)
	}
}

func TestErrorKindSurfaces(t *testing.T) {
	fake := startFakeCore(t, func(method string, params json.RawMessage) (any, *Error) {
		return nil, &Error{Code: -32000, Message: "terminal is blocked", Data: ErrorData{Kind: "blocked", Hint: "answer the dialog"}}
	})
	c := NewAt(fake.socket())
	_, err := c.Send(context.Background(), "t2", "text", SendParams{})
	if err == nil {
		t.Fatal("want error")
	}
	var hcErr *Error
	if !errors.As(err, &hcErr) {
		t.Fatalf("want *Error, got %T: %v", err, err)
	}
	if hcErr.Data.Kind != "blocked" || hcErr.Data.Hint != "answer the dialog" {
		t.Fatalf("error %+v", hcErr)
	}
}

func TestMachineAddressesAreRefusedWithoutDialing(t *testing.T) {
	// A socket path that cannot exist: any dial attempt fails loudly.
	c := NewAt(filepath.Join(t.TempDir(), "absent.sock"))
	_, err := c.Get(context.Background(), "machine/t7")
	if err == nil || !errors.Is(err, ErrMachineAddress) {
		t.Fatalf("want ErrMachineAddress, got %v", err)
	}
	if _, err := c.Send(context.Background(), "machine/t9", "x", SendParams{}); !errors.Is(err, ErrMachineAddress) {
		t.Fatalf("want ErrMachineAddress, got %v", err)
	}
}

func TestSocketDiscoveryOrder(t *testing.T) {
	// HC_SOCKET wins over everything.
	t.Setenv("HC_SOCKET", "/tmp/hc-override.sock")
	t.Setenv("XDG_RUNTIME_DIR", "/tmp/runtime")
	t.Setenv("HC_SESSION", "probe")
	if got := SocketPath(); got != "/tmp/hc-override.sock" {
		t.Fatalf("HC_SOCKET must win, got %q", got)
	}
	// XDG_RUNTIME_DIR + session naming without HC_SOCKET.
	os.Unsetenv("HC_SOCKET")
	if got := SocketPath(); got != "/tmp/runtime/herdr-core/probe.sock" {
		t.Fatalf("default path %q", got)
	}
	// No XDG_RUNTIME_DIR: /run/user/$UID.
	os.Unsetenv("XDG_RUNTIME_DIR")
	got := SocketPath()
	if !filepath.IsAbs(got) || !strings.Contains(got, filepath.Join("herdr-core", "probe.sock")) {
		t.Fatalf("fallback path %q", got)
	}
}

func TestSubscribeStreamsNotifications(t *testing.T) {
	ln, err := net.Listen("unix", filepath.Join(t.TempDir(), "sub.sock"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		line, _ := bufio.NewReader(conn).ReadString('\n')
		var req struct {
			ID json.RawMessage `json:"id"`
		}
		if json.Unmarshal([]byte(line), &req) != nil {
			return
		}
		resp, _ := json.Marshal(map[string]any{
			"jsonrpc": "2.0", "id": req.ID,
			"result": map[string]any{"seq": 4, "terminals": []any{}, "channel": nil},
		})
		if _, err := conn.Write(append(resp, '\n')); err != nil {
			return
		}
		for i := 5; i <= 7; i++ {
			note, _ := json.Marshal(map[string]any{
				"jsonrpc": "2.0", "method": "event",
				"params": map[string]any{
					"seq": i, "terminal": "t2", "type": "status",
					"data": map[string]any{"state": "working", "app": "pi"},
				},
			})
			if _, err := conn.Write(append(note, '\n')); err != nil {
				return
			}
			time.Sleep(10 * time.Millisecond)
		}
		<-time.After(2 * time.Second)
	}()
	c := NewAt(ln.Addr().String())
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stream, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close()
	if stream.Seq != 4 {
		t.Fatalf("seq %d", stream.Seq)
	}
	var got []Event
	timeout := time.After(3 * time.Second)
	for len(got) < 3 {
		select {
		case ev := <-stream.Events():
			got = append(got, ev)
		case <-timeout:
			t.Fatalf("timed out with %d events", len(got))
		}
	}
	if got[0].Type != "status" || got[0].Terminal != "t2" || got[0].Seq != 5 {
		t.Fatalf("first event %+v", got[0])
	}
	if got[0].Data["state"] != "working" {
		t.Fatalf("event data %v", got[0].Data)
	}
}
