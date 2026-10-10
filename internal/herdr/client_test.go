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
		if m["method"] != "agent.list" || m["id"] == nil {
			t.Errorf("bad request framing: %s", req)
		}
		return []byte("{\"id\":\"piremote-1\",\"result\":" + agentsResult + "}\n")
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
