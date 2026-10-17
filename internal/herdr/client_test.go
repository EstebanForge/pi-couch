package herdr

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// fakeSocket serves one canned reply per line received.
func fakeSocket(t *testing.T, handler func(t *testing.T, req []byte) []byte) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "herdr.sock")
	ln, err := net.Listen("unix", path)
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
			go func() {
				defer conn.Close()
				line, err := bufio.NewReader(conn).ReadBytes('\n')
				if err != nil {
					return
				}
				resp := handler(t, line)
				if resp != nil {
					_, _ = conn.Write(resp)
				}
			}()
		}
	}()
	return path
}

const agentsResult = `{"agents":[{"name":"a","agent":"pi","agent_status":"working","pane_id":"p1","agent_session":{"kind":"path","value":"/s/one.jsonl"}},{"name":"b","agent":"pi","agent_status":"done","pane_id":"p2","agent_session":null}]}`

func TestAgentsAndBadges(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m map[string]any
		if err := json.Unmarshal(bytes.TrimSuffix(req, []byte("\n")), &m); err != nil {
			t.Errorf("request not one JSON line: %v", err)
		}
		if (m["method"] != "agent.list" && m["method"] != "workspace.list") || m["id"] == nil {
			t.Errorf("bad request framing: %s", req)
		}
		return []byte("{\"id\":\"couch-pi-1\",\"result\":" + agentsResult + "}\n")
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	agents, err := c.Agents(context.Background())
	if err != nil || len(agents) != 2 {
		t.Fatalf("Agents: %v, %d", err, len(agents))
	}
	badges := c.Badges(context.Background())
	if badges["/s/one.jsonl"] != "working" {
		t.Fatalf("badges: %v", badges)
	}
	if _, ok := badges["/s/none.jsonl"]; ok {
		t.Fatal("session-less agent must not badge")
	}
}

func TestErrorResponse(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		return []byte(`{"id":"x","error":{"code":-1,"message":"nope"}}` + "\n")
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	if _, err := c.Agents(context.Background()); err == nil {
		t.Fatal("error response must surface")
	}
}

func TestTimeout(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		time.Sleep(3 * time.Second)
		return nil
	})
	c := &Client{path: path, timeout: 200 * time.Millisecond}
	if _, err := c.Agents(context.Background()); err == nil {
		t.Fatal("timeout must surface")
	}
}

func TestDeadSocketIsEmptyNotError(t *testing.T) {
	c := &Client{path: filepath.Join(t.TempDir(), "gone.sock"), timeout: 200 * time.Millisecond}
	// Agents reports the dial error; Badges swallows it for UI use.
	if _, err := c.Agents(context.Background()); err == nil {
		t.Fatal("dial error must surface from Agents")
	}
	if badges := c.Badges(context.Background()); len(badges) != 0 {
		t.Fatalf("Badges must be empty on error, got %v", badges)
	}
}

func TestPromptAndSendKeys(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			ID     string `json:"id"`
			Method string `json:"method"`
			Params struct {
				Target string   `json:"target"`
				Text   string   `json:"text"`
				Keys   []string `json:"keys"`
			} `json:"params"`
		}
		if err := json.Unmarshal(bytes.TrimSuffix(req, []byte("\n")), &m); err != nil {
			t.Errorf("bad request: %v", err)
		}
		switch m.Method {
		case "agent.prompt":
			if m.Params.Target != "wX:p1" || m.Params.Text != "hello" {
				t.Errorf("prompt params: %+v", m.Params)
			}
		case "agent.send_keys":
			if m.Params.Target != "wX:p1" || len(m.Params.Keys) != 1 || m.Params.Keys[0] != "ctrl+c" {
				t.Errorf("send_keys params: %+v", m.Params)
			}
		default:
			t.Errorf("unexpected method %q", m.Method)
		}
		return []byte("{\"id\":\"x\",\"result\":{\"type\":\"ok\"}}\n")
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	if err := c.Prompt(context.Background(), "wX:p1", "hello"); err != nil {
		t.Fatalf("Prompt: %v", err)
	}
	if err := c.SendKeys(context.Background(), "wX:p1", []string{"ctrl+c"}); err != nil {
		t.Fatalf("SendKeys: %v", err)
	}
}

func TestTargetForPath(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		return []byte("{\"id\":\"x\",\"result\":" + agentsResult + "}\n")
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	target, err := c.TargetForPath(context.Background(), "/s/one.jsonl")
	if err != nil || target != "p1" {
		t.Fatalf("TargetForPath: %v %q", err, target)
	}
	if _, err := c.TargetForPath(context.Background(), "/s/missing.jsonl"); err != ErrNotDriven {
		t.Fatalf("missing session: %v", err)
	}
}

func TestPaneReadAndSendInput(t *testing.T) {
	path := fakeSocket(t, func(t *testing.T, req []byte) []byte {
		var m struct {
			Method string `json:"method"`
			Params struct {
				PaneID string          `json:"pane_id"`
				Source string          `json:"source"`
				Lines  int             `json:"lines"`
				Text   string          `json:"text"`
				Keys   json.RawMessage `json:"keys"`
			} `json:"params"`
		}
		if err := json.Unmarshal(bytes.TrimSuffix(req, []byte("\n")), &m); err != nil {
			t.Errorf("bad request: %v", err)
		}
		switch m.Method {
		case "pane.read":
			if m.Params.PaneID != "wX:p1" || m.Params.Source != "recent" || m.Params.Lines != 400 {
				t.Errorf("read params: %+v", m.Params)
			}
			return []byte(`{"id":"x","result":{"type":"pane_read","read":{"pane_id":"wX:p1","source":"recent","format":"ansi","text":"\\u001b[32mhi\\u001b[0m","revision":7,"truncated":false}}}` + "\n")
		case "pane.send_input":
			if m.Params.PaneID != "wX:p1" || m.Params.Text != "ls\r" {
				t.Errorf("input params: %+v", m.Params)
			}
			return []byte("{\"id\":\"x\",\"result\":{\"type\":\"ok\"}}\n")
		default:
			t.Errorf("unexpected method %q", m.Method)
			return nil
		}
	})
	c := &Client{path: path, timeout: 2 * time.Second}
	read, err := c.PaneRead(context.Background(), "wX:p1", "recent", 400)
	if err != nil {
		t.Fatalf("PaneRead: %v", err)
	}
	if read.Revision != 7 || read.Text == "" {
		t.Fatalf("read: %+v", read)
	}
	if err := c.PaneSendInput(context.Background(), "wX:p1", "ls\r", nil); err != nil {
		t.Fatalf("PaneSendInput: %v", err)
	}
}

func TestDefaultSocketPath(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no home")
	}
	want := filepath.Join(home, ".config", "herdr", "herdr.sock")
	if DefaultSocketPath() != want {
		t.Fatalf("DefaultSocketPath: %q", DefaultSocketPath())
	}
}
