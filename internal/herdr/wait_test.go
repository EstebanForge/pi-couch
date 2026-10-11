package herdr

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

func TestAgentWaitReturnsOnTransition(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		if err := json.Unmarshal(req, &m); err != nil {
			t.Fatal(err)
		}
		if m.Method != "agent.wait" {
			t.Fatalf("method %q", m.Method)
		}
		if m.Params["target"] != "w13:p1" {
			t.Errorf("target %v", m.Params["target"])
		}
		until, ok := m.Params["until"].([]any)
		if !ok || len(until) != 2 || until[0] != "done" || until[1] != "blocked" {
			t.Errorf("until %v", m.Params["until"])
		}
		if ms, ok := m.Params["timeout_ms"].(float64); !ok || ms != 20000 {
			t.Errorf("timeout_ms %v", m.Params["timeout_ms"])
		}
		agent := map[string]any{
			"name":         "probe",
			"agent":        "pi",
			"agent_status": "done",
			"pane_id":      "w13:p1",
			"agent_session": map[string]any{
				"kind":  "path",
				"value": "/tmp/s.jsonl",
			},
		}
		out, _ := json.Marshal(map[string]any{"id": "x", "result": map[string]any{"type": "agent_waited", "agent": agent}})
		return append(out, '\n')
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	ag, err := c.AgentWait(context.Background(), "w13:p1", []string{"done", "blocked"}, 20*time.Second)
	if err != nil {
		t.Fatalf("AgentWait: %v", err)
	}
	if ag.AgentStatus != "done" || ag.Session == nil || ag.Session.Value != "/tmp/s.jsonl" {
		t.Fatalf("agent: %+v", ag)
	}
}

func TestAgentWaitTimeoutIsError(t *testing.T) {
	path := fakeSocket(t, func(_ *testing.T, _ []byte) []byte {
		return []byte(`{"id":"x","error":{"code":"timeout","message":"timed out waiting for agent status"}}` + "\n")
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	if _, err := c.AgentWait(context.Background(), "w13:p1", []string{"done"}, time.Second); err == nil {
		t.Fatal("want timeout error")
	}
}
