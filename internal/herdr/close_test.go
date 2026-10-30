package herdr

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"
	"time"
)

func TestWorkspaceForPathReturnsWorkspaceID(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(req, &m); err != nil {
			t.Fatal(err)
		}
		if m.Method != "agent.list" {
			t.Fatalf("method %q", m.Method)
		}
		agents := []map[string]any{
			{
				"name":         "driven",
				"agent":        "pi",
				"agent_status": "working",
				"pane_id":      "wF:p2",
				"workspace_id": "wF",
				"agent_session": map[string]any{
					"kind":  "path",
					"value": "/tmp/driven.jsonl",
				},
			},
			{
				"name":         "bare",
				"agent":        "pi",
				"agent_status": "idle",
				"pane_id":      "wT:p1",
				"workspace_id": "wT",
			},
		}
		out, _ := json.Marshal(map[string]any{"id": "x", "result": map[string]any{"agents": agents}})
		return append(out, '\n')
	})
	c := &Client{path: path, timeout: 2 * time.Second}

	ws, err := c.WorkspaceForPath(context.Background(), "/tmp/driven.jsonl")
	if err != nil {
		t.Fatalf("WorkspaceForPath: %v", err)
	}
	if ws != "wF" {
		t.Fatalf("workspace %q", ws)
	}
	if _, err := c.WorkspaceForPath(context.Background(), "/tmp/other.jsonl"); !errors.Is(err, ErrNotDriven) {
		t.Fatalf("want ErrNotDriven, got %v", err)
	}
}

func TestWorkspaceCloseSendsID(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(req, &m); err != nil {
			t.Fatal(err)
		}
		if m.Method != "workspace.close" {
			t.Fatalf("method %q", m.Method)
		}
		if m.Params["workspace_id"] != "wF" {
			t.Errorf("workspace_id %v", m.Params["workspace_id"])
		}
		if _, ok := m.Params["close_group"]; ok {
			t.Errorf("close_group sent: %v", m.Params["close_group"])
		}
		out, _ := json.Marshal(map[string]any{"id": "x", "result": map[string]any{"ok": true}})
		return append(out, '\n')
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	if err := c.WorkspaceClose(context.Background(), "wF"); err != nil {
		t.Fatalf("WorkspaceClose: %v", err)
	}
}

func TestClientStopSendsCtrlC(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(req, &m); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		methods = append(methods, m.Method)
		mu.Unlock()
		out, _ := json.Marshal(map[string]any{"id": "x", "result": map[string]any{}})
		return append(out, '\n')
	})
	c := &Client{path: path, timeout: 2 * time.Second}

	if err := c.Stop(context.Background(), "wF:p2"); err != nil {
		t.Fatalf("stop: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(methods) != 1 || methods[0] != "agent.send_keys" {
		t.Fatalf("methods = %v, want one agent.send_keys", methods)
	}
}

func TestClientCloseExitsPiThenTheShell(t *testing.T) {
	var mu sync.Mutex
	var methods []string
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			Method string `json:"method"`
		}
		if err := json.Unmarshal(req, &m); err != nil {
			t.Fatal(err)
		}
		mu.Lock()
		methods = append(methods, m.Method)
		mu.Unlock()
		out, _ := json.Marshal(map[string]any{"id": "x", "result": map[string]any{}})
		return append(out, '\n')
	})
	c := &Client{path: path, timeout: 2 * time.Second, closeGrace: 20 * time.Millisecond}

	if err := c.Close(context.Background(), "wF:p2"); err != nil {
		t.Fatalf("close: %v", err)
	}
	mu.Lock()
	defer mu.Unlock()
	want := []string{"pane.send_input", "pane.send_input", "pane.close"}
	if len(methods) != len(want) {
		t.Fatalf("methods = %v, want %v", methods, want)
	}
	for i, m := range want {
		if methods[i] != m {
			t.Fatalf("methods = %v, want %v", methods, want)
		}
	}
}
