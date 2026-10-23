package herdr

import (
	"encoding/json"
	"testing"
	"time"
)

// Live schema (herdr api schema --json): workspace.create takes label,
// cwd, focus (default false); agent.start requires name, kind, pane_id.
// The created result carries workspace_id and root_pane.pane_id.

func TestWorkspaceCreateSendsLabelAndCwd(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(req, &m); err != nil {
			t.Fatal(err)
		}
		if m.Method != "workspace.create" {
			t.Fatalf("method %q", m.Method)
		}
		if m.Params["label"] != "demo" {
			t.Errorf("label %v", m.Params["label"])
		}
		if m.Params["cwd"] != "/tmp/demo" {
			t.Errorf("cwd %v", m.Params["cwd"])
		}
		if m.Params["focus"] != false {
			t.Errorf("focus %v, want false so the desk keeps its pane", m.Params["focus"])
		}
		out, _ := json.Marshal(map[string]any{"id": "x", "result": map[string]any{
			"type":      "workspace_created",
			"workspace": map[string]any{"workspace_id": "wZ"},
			"root_pane": map[string]any{"pane_id": "wZ:p1"},
		}})
		return append(out, '\n')
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	id, pane, err := c.WorkspaceCreate(t.Context(), "demo", "/tmp/demo")
	if err != nil {
		t.Fatal(err)
	}
	if id != "wZ" || pane != "wZ:p1" {
		t.Errorf("got id=%q pane=%q", id, pane)
	}
}

func TestAgentStartSendsNameKindPane(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(req, &m); err != nil {
			t.Fatal(err)
		}
		if m.Method != "agent.start" {
			t.Fatalf("method %q", m.Method)
		}
		if m.Params["name"] != "probe" || m.Params["kind"] != "pi" || m.Params["pane_id"] != "wZ:p1" {
			t.Errorf("params %v", m.Params)
		}
		args, ok := m.Params["args"].([]any)
		if !ok || len(args) != 2 || args[0] != "--model" || args[1] != "x/y:high" {
			t.Errorf("args %v", m.Params["args"])
		}
		out, _ := json.Marshal(map[string]any{"id": "x", "result": map[string]any{
			"agent": map[string]any{"agent": "pi", "agent_status": "idle", "pane_id": "wZ:p1"},
		}})
		return append(out, '\n')
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	a, err := c.AgentStart(t.Context(), "probe", "pi", "wZ:p1", []string{"--model", "x/y:high"})
	if err != nil {
		t.Fatal(err)
	}
	if a.Agent != "pi" || a.PaneID != "wZ:p1" {
		t.Errorf("agent %+v", a)
	}
}
