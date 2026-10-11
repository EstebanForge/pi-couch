// Package herdr talks to the herdr socket API. Protocol 22, NDJSON framing
// over the unix socket: {"id","method","params"} in, {"id","result"} out.
package herdr

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"
)

// Agent mirrors the herdr agent object fields Couch Pi consumes.
type Agent struct {
	Name        string           `json:"name"`
	Agent       string           `json:"agent"`
	AgentStatus string           `json:"agent_status"`
	PaneID      string           `json:"pane_id"`
	Session     *AgentSessionRef `json:"agent_session"`
}

// AgentSessionRef carries the pi session file path once a prompt ran.
type AgentSessionRef struct {
	Kind  string `json:"kind"`
	Value string `json:"value"`
}

type response struct {
	ID     string          `json:"id"`
	Result json.RawMessage `json:"result"`
	Error  *struct {
		Message string `json:"message"`
	} `json:"error"`
}

// Client is a one-shot request client over the herdr socket. Safe for
// concurrent use: each call opens its own connection.
type Client struct {
	path    string
	timeout time.Duration
	seq     atomic.Int64
}

func DefaultSocketPath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "herdr", "herdr.sock")
}

func New() *Client {
	return &Client{path: DefaultSocketPath(), timeout: 3 * time.Second}
}

// Agents returns the live agent list. A missing or dead socket returns an
// empty list and no error: badges are additive UI, never a page failure.
func (c *Client) Agents(ctx context.Context) ([]Agent, error) {
	raw, err := c.call(ctx, "agent.list", map[string]any{})
	if err != nil {
		return nil, err
	}
	var result struct {
		Agents []Agent `json:"agents"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("herdr: decode agent.list: %w", err)
	}
	return result.Agents, nil
}

// Badges maps pi session file paths to agent status strings.
func (c *Client) Badges(ctx context.Context) map[string]string {
	agents, err := c.Agents(ctx)
	if err != nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(agents))
	for _, a := range agents {
		if a.Session != nil && a.Session.Value != "" && a.AgentStatus != "" {
			out[a.Session.Value] = a.AgentStatus
		}
	}
	return out
}

// TargetForPath returns the pane id of the agent driving the session file.
// Pane ids are verified targets for prompt and send_keys (M1 probe).
func (c *Client) TargetForPath(ctx context.Context, path string) (string, error) {
	agents, err := c.Agents(ctx)
	if err != nil {
		return "", err
	}
	for _, a := range agents {
		if a.Session != nil && a.Session.Value == path {
			return a.PaneID, nil
		}
	}
	return "", ErrNotDriven
}

// ErrNotDriven means no herdr agent currently runs that session file.
var ErrNotDriven = errors.New("herdr: no agent runs this session")

// Prompt submits text to the agent. pi queues it when working: steering
// rides on pi's queue mode, no UI toggle (ADR-0001).
func (c *Client) Prompt(ctx context.Context, target, text string) error {
	_, err := c.call(ctx, "agent.prompt", map[string]any{"target": target, "text": text})
	return err
}

// SendKeys delivers key presses; abort is SendKeys(ctx, target, ["ctrl+c"])
// per ADR-0001, not an API verb.
func (c *Client) SendKeys(ctx context.Context, target string, keys []string) error {
	_, err := c.call(ctx, "agent.send_keys", map[string]any{"target": target, "keys": keys})
	return err
}

// PaneRead is one pane.read result: ansi text plus the pane revision.
type PaneRead struct {
	PaneID    string `json:"pane_id"`
	Source    string `json:"source"`
	Format    string `json:"format"`
	Text      string `json:"text"`
	Revision  int64  `json:"revision"`
	Truncated bool   `json:"truncated"`
}

// PaneRead reads pane content. herdr 0.9.3 has no output push: subscribe
// sockets ack only and events.wait takes agent-status matches only, so the
// terminal stream polls and gates on Revision.
func (c *Client) PaneRead(ctx context.Context, paneID, source string, lines int) (PaneRead, error) {
	params := map[string]any{"pane_id": paneID, "source": source, "format": "ansi"}
	if lines > 0 {
		params["lines"] = lines
	}
	res, err := c.call(ctx, "pane.read", params)
	if err != nil {
		return PaneRead{}, err
	}
	var out struct {
		Read PaneRead `json:"read"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return PaneRead{}, err
	}
	return out.Read, nil
}

// PaneSendInput writes text and/or key names straight into the pane.
func (c *Client) PaneSendInput(ctx context.Context, paneID, text string, keys []string) error {
	params := map[string]any{"pane_id": paneID}
	if text != "" {
		params["text"] = text
	}
	if len(keys) > 0 {
		params["keys"] = keys
	}
	_, err := c.call(ctx, "pane.send_input", params)
	return err
}

func (c *Client) call(ctx context.Context, method string, params any) (json.RawMessage, error) {
	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, fmt.Errorf("herdr: dial: %w", err)
	}
	defer conn.Close()
	deadline := time.Now().Add(c.timeout)
	if dl, ok := ctx.Deadline(); ok && dl.Before(deadline) {
		deadline = dl
	}
	_ = conn.SetDeadline(deadline)

	id := fmt.Sprintf("couch-pi-%d", c.seq.Add(1))
	req, err := json.Marshal(map[string]any{"id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return nil, fmt.Errorf("herdr: write: %w", err)
	}

	line, err := bufio.NewReaderSize(conn, 64*1024).ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return nil, fmt.Errorf("herdr: read: %w", err)
	}
	var resp response
	if err := json.Unmarshal(line, &resp); err != nil {
		return nil, fmt.Errorf("herdr: decode %s response: %w", method, err)
	}
	if resp.Error != nil {
		return nil, fmt.Errorf("herdr: %s: %s", method, resp.Error.Message)
	}
	if resp.Result == nil {
		return nil, errors.New("herdr: empty result")
	}
	return resp.Result, nil
}
