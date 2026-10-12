package herdr

import (
	"context"
	"encoding/json"
	"errors"
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
