// Package hc talks to the herdr-core (hc) socket API: JSON-RPC 2.0 over
// a unix socket, one request per dial (ADR-0008). Method set is a hard
// allowlist: the hc socket grants a same-user process the whole API
// (machine.exec, file.write), so this client only exposes what Couch
// Pi needs and never forwards raw params from the web.
package hc

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Status is hc's view of a program's OSC 7501 report. State is one of
// idle, working, done, blocked, error; kind (permission, question, auth)
// rides only on blocked.
type Status struct {
	State    string `json:"state"`
	Kind     string `json:"kind,omitempty"`
	Progress *int   `json:"progress,omitempty"`
	App      string `json:"app,omitempty"`
	Title    string `json:"title,omitempty"`
	Msg      string `json:"msg,omitempty"`
}

// Terminal mirrors hc's public terminal record, the fields Couch Pi
// consumes.
type Terminal struct {
	Address    string            `json:"address"`
	Name       *string           `json:"name"`
	Labels     map[string]string `json:"labels"`
	Argv       []string          `json:"argv"`
	Cwd        string            `json:"cwd"`
	State      string            `json:"state"`
	Pid        *int              `json:"pid"`
	ExitCode   *int              `json:"exit_code"`
	Signal     *int              `json:"signal"`
	Reason     *string           `json:"reason"`
	CreatedMs  int64             `json:"created_ms"`
	FinishedMs *int64            `json:"finished_ms"`
	Cols       int               `json:"cols"`
	Rows       int               `json:"rows"`
	Identity   string            `json:"identity"`
	Status     *Status           `json:"status"`
	StatusMs   *int64            `json:"status_ms"`
	Records    map[string]Status `json:"records"`
	Turn       int64             `json:"turn"`
	Reported   bool              `json:"reported"`
	Title      string            `json:"title"`
	Notify     bool              `json:"notify"`
	Keep       bool              `json:"keep"`
}

// WaitResult is hc's turn-wait outcome: how the watched turn ended.
type WaitResult struct {
	Terminal string       `json:"terminal"`
	Ended    bool         `json:"ended"`
	Reason   string       `json:"reason"`
	Status   *string      `json:"status"`
	Turn     int64        `json:"turn"`
	ExitCode *int         `json:"exit_code"`
	Signal   *int         `json:"signal"`
	Children []WaitResult `json:"children"`
	From     *string      `json:"from"`
}

// SendResult reports whether a pasted prompt started working and how
// its turn ended when wait was set.
type SendResult struct {
	Started *bool       `json:"started"`
	Status  *string     `json:"status"`
	Turn    int64       `json:"turn"`
	Wait    *WaitResult `json:"wait"`
}

// ReadResult is terminal.read's plain-text screen render.
type ReadResult struct {
	Text     string `json:"text"`
	Complete bool   `json:"complete"`
	How      string `json:"how"`
	Why      string `json:"why"`
}

// InterruptResult reports the outcome of terminal.interrupt: hc presses
// esc for status-reporting programs (pi) and ctrl+c otherwise.
type InterruptResult struct {
	Outcome string `json:"outcome"`
	Key     string `json:"key"`
}

// RunParams starts a program in a new terminal.
type RunParams struct {
	Argv   []string          `json:"argv,omitempty"`
	Line   string            `json:"line,omitempty"`
	Name   string            `json:"name,omitempty"`
	Cwd    string            `json:"cwd,omitempty"`
	Env    map[string]string `json:"env,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
	RM     bool              `json:"rm,omitempty"`
	Keep   bool              `json:"keep,omitempty"`
}

// ListParams filters terminal.list.
type ListParams struct {
	All    bool              `json:"all,omitempty"`
	Labels map[string]string `json:"labels,omitempty"`
}

// SendParams tunes terminal.send.
type SendParams struct {
	// Force types even when the program is blocked (dialog answering).
	Force bool
	// OnlyIf types only when the status word matches ("idle", "working").
	OnlyIf string
	// Wait holds the call until the started turn ends.
	Wait bool
	// Timeout bounds Wait; hc still owns its own quiet-and-settle pacing.
	Timeout time.Duration
}

// ErrorData carries hc's stable error kind (blocked, not_found, ...)
// and hint. Clients branch on Kind, never on message text.
type ErrorData struct {
	Kind string `json:"kind"`
	Hint string `json:"hint"`
}

// Error is hc's JSON-RPC error object: {"code","message","data":{"kind","hint"}}.
type Error struct {
	Code    int64     `json:"code"`
	Message string    `json:"message"`
	Data    ErrorData `json:"data"`
}

func (e *Error) Error() string {
	if e.Data.Hint != "" {
		return fmt.Sprintf("hc: %s (%s)", e.Message, e.Data.Hint)
	}
	return "hc: " + e.Message
}

// ErrMachineAddress marks a machine/t7 address: this client is local-only.
var ErrMachineAddress = errors.New("hc: remote machine addresses are not supported")

func checkAddress(address string) error {
	if strings.Contains(address, "/") {
		return ErrMachineAddress
	}
	return nil
}

// Client issues one request per dial over the hc socket. Safe for
// concurrent use.
type Client struct {
	path    string
	timeout time.Duration
	seq     atomic.Int64
}

// SocketPath resolves the hc socket the same way hc does: HC_SOCKET
// wins, then $XDG_RUNTIME_DIR (else /run/user/$UID), with HC_SESSION
// naming an isolated core instead of default.
func SocketPath() string {
	if p := os.Getenv("HC_SOCKET"); p != "" {
		return p
	}
	base := os.Getenv("XDG_RUNTIME_DIR")
	if base == "" {
		base = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	session := os.Getenv("HC_SESSION")
	if session == "" {
		session = "default"
	}
	return filepath.Join(base, "herdr-core", session+".sock")
}

// New points the client at the discovered hc socket.
func New() *Client {
	return &Client{path: SocketPath(), timeout: 10 * time.Second}
}

// NewAt points the client at a specific socket path. Tests use it to
// run a fake core on a temp socket.
func NewAt(path string) *Client {
	return &Client{path: path, timeout: 10 * time.Second}
}

// SocketPath exposes the resolved socket for diagnostics.
func (c *Client) SocketPath() string { return c.path }

// Run starts a program in a new terminal and returns its record.
func (c *Client) Run(ctx context.Context, p RunParams) (Terminal, error) {
	var out Terminal
	res, err := c.call(ctx, "terminal.run", p, 0)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(res, &out)
}

// List returns matching terminals, live only unless All.
func (c *Client) List(ctx context.Context, p ListParams) ([]Terminal, error) {
	res, err := c.call(ctx, "terminal.list", p, 0)
	if err != nil {
		return nil, err
	}
	var out struct {
		Terminals []Terminal `json:"terminals"`
	}
	if err := json.Unmarshal(res, &out); err != nil {
		return nil, fmt.Errorf("hc: decode terminal.list: %w", err)
	}
	return out.Terminals, nil
}

// Get returns one terminal's record.
func (c *Client) Get(ctx context.Context, address string) (Terminal, error) {
	if err := checkAddress(address); err != nil {
		return Terminal{}, err
	}
	var out Terminal
	res, err := c.call(ctx, "terminal.get", map[string]any{"terminal": address}, 0)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(res, &out)
}

// Read renders the terminal screen plus lines of history as text.
func (c *Client) Read(ctx context.Context, address string, lines int) (ReadResult, error) {
	if err := checkAddress(address); err != nil {
		return ReadResult{}, err
	}
	var out ReadResult
	res, err := c.call(ctx, "terminal.read", map[string]any{"terminal": address, "lines": lines}, 0)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(res, &out)
}

// Send pastes text and presses Enter. hc waits ~3s of human quiet plus
// settle before returning, and with Wait set holds through the started
// turn, so the call budget is the requested timeout plus slack.
func (c *Client) Send(ctx context.Context, address, text string, p SendParams) (SendResult, error) {
	if err := checkAddress(address); err != nil {
		return SendResult{}, err
	}
	// caller stays unset on purpose: hc wraps sent text in <hc from=...>
	// tags when a caller is named.
	params := map[string]any{"terminal": address, "text": text}
	if p.Force {
		params["force"] = true
	}
	if p.OnlyIf != "" {
		params["if"] = p.OnlyIf
	}
	if p.Wait {
		params["wait"] = true
	}
	if p.Timeout > 0 {
		params["timeout"] = p.Timeout.Seconds()
	}
	var out SendResult
	res, err := c.call(ctx, "terminal.send", params, p.Timeout+30*time.Second)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(res, &out)
}

// Keys presses named keys (enter, esc, ctrl+c, ...), honoring hc's
// human-quiet rule.
func (c *Client) Keys(ctx context.Context, address string, keys []string) error {
	if err := checkAddress(address); err != nil {
		return err
	}
	_, err := c.call(ctx, "terminal.keys", map[string]any{"terminal": address, "keys": keys}, 15*time.Second)
	return err
}

// Interrupt presses the stop key and reports what was observed.
func (c *Client) Interrupt(ctx context.Context, address string, timeout time.Duration) (InterruptResult, error) {
	if err := checkAddress(address); err != nil {
		return InterruptResult{}, err
	}
	params := map[string]any{"terminal": address}
	if timeout > 0 {
		params["timeout"] = timeout.Seconds()
	}
	var out InterruptResult
	res, err := c.call(ctx, "terminal.interrupt", params, timeout+15*time.Second)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(res, &out)
}

// Wait holds until the terminal is not working.
func (c *Client) Wait(ctx context.Context, address string, timeout time.Duration) (WaitResult, error) {
	if err := checkAddress(address); err != nil {
		return WaitResult{}, err
	}
	params := map[string]any{"terminal": address}
	if timeout > 0 {
		params["timeout"] = timeout.Seconds()
	}
	var out WaitResult
	res, err := c.call(ctx, "terminal.wait", params, timeout+30*time.Second)
	if err != nil {
		return out, err
	}
	return out, json.Unmarshal(res, &out)
}

// Label sets labels on a terminal; a key with an empty value removes it.
func (c *Client) Label(ctx context.Context, address string, labels map[string]string) error {
	if err := checkAddress(address); err != nil {
		return err
	}
	_, err := c.call(ctx, "terminal.label", map[string]any{"terminal": address, "labels": labels}, 0)
	return err
}

// Kill ends a terminal's program.
func (c *Client) Kill(ctx context.Context, address string) error {
	if err := checkAddress(address); err != nil {
		return err
	}
	_, err := c.call(ctx, "terminal.kill", map[string]any{"terminal": address}, 0)
	return err
}

// Remove drops a finished terminal's record.
func (c *Client) Remove(ctx context.Context, address string) error {
	if err := checkAddress(address); err != nil {
		return err
	}
	_, err := c.call(ctx, "terminal.rm", map[string]any{"terminal": address}, 0)
	return err
}

// Event is one events.subscribe notification.
type Event struct {
	Seq      int64          `json:"seq"`
	Type     string         `json:"type"`
	Terminal string         `json:"terminal"`
	Data     map[string]any `json:"data"`
}

// EventStream is a plain-mode events subscription: the connection
// carries only notifications after the reply. Reconnect is the
// caller's concern (the session map re-subscribes and replays).
type EventStream struct {
	Seq       int64
	Terminals []Terminal
	events    chan Event
	conn      net.Conn
	closeOnce sync.Once
	done      chan struct{}
}

// Events yields notifications as they arrive.
func (s *EventStream) Events() <-chan Event { return s.events }

// Done closes when the stream ends (socket loss or a read error): the
// events channel never closes on its own.
func (s *EventStream) Done() <-chan struct{} { return s.done }

// Close drops the subscription connection.
func (s *EventStream) Close() { s.closeOnce.Do(func() { _ = s.conn.Close() }) }

// Subscribe opens the events connection. The context bounds only the
// handshake; the stream lives until Close or socket loss.
func (c *Client) Subscribe(ctx context.Context) (*EventStream, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	s := &EventStream{events: make(chan Event, 64), conn: conn, done: make(chan struct{})}
	id := fmt.Sprintf("couch-pi-%d", c.seq.Add(1))
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": "events.subscribe", "params": map[string]any{}})
	if err != nil {
		_ = conn.Close()
		return nil, err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("hc: write: %w", err)
	}
	// The reply is the next line; notifications only follow it. One
	// reader for both: a bufio handshake read can buffer past the reply
	// line, and those bytes must flow into the stream reader.
	reader := bufio.NewReader(conn)
	line, err := reader.ReadString('\n')
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("hc: read events.subscribe reply: %w", err)
	}
	var resp struct {
		Result *struct {
			Seq       int64      `json:"seq"`
			Terminals []Terminal `json:"terminals"`
		} `json:"result"`
		Error *Error `json:"error"`
	}
	if err := json.Unmarshal([]byte(line), &resp); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("hc: decode events.subscribe: %w", err)
	}
	if resp.Error != nil {
		_ = conn.Close()
		return nil, resp.Error
	}
	if resp.Result == nil {
		_ = conn.Close()
		return nil, errors.New("hc: empty events.subscribe result")
	}
	s.Seq = resp.Result.Seq
	s.Terminals = resp.Result.Terminals
	go s.read(reader)
	return s, nil
}

func (s *EventStream) read(reader *bufio.Reader) {
	defer close(s.done)
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 4*1024*1024)
	for scanner.Scan() {
		var note struct {
			Method string `json:"method"`
			Params struct {
				Event
			} `json:"params"`
		}
		if json.Unmarshal(scanner.Bytes(), &note) != nil {
			continue
		}
		if note.Method == "missed" {
			// The core dropped our backlog: the only safe resync is a
			// fresh snapshot, so end the stream and let Run resubscribe.
			return
		}
		if note.Method != "event" {
			continue
		}
		select {
		case s.events <- note.Params.Event:
		case <-s.done:
			return
		}
	}
}

func (c *Client) dial(ctx context.Context) (net.Conn, error) {
	dialer := net.Dialer{Timeout: c.timeout}
	conn, err := dialer.DialContext(ctx, "unix", c.path)
	if err != nil {
		return nil, fmt.Errorf("hc: dial %s: %w", c.path, err)
	}
	return conn, nil
}

// call issues one request on its own connection. d bounds the exchange
// beyond the context: 0 uses the client default, negative means
// context-only, positive adds that slack.
func (c *Client) call(ctx context.Context, method string, params any, slack time.Duration) (json.RawMessage, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	deadline := time.Now().Add(c.timeout)
	if slack > 0 {
		deadline = time.Now().Add(c.timeout + slack)
	} else if slack < 0 {
		deadline = time.Time{}
	}
	if dl, ok := ctx.Deadline(); ok && (deadline.IsZero() || dl.Before(deadline)) {
		deadline = dl
	}
	if !deadline.IsZero() {
		_ = conn.SetDeadline(deadline)
	}

	id := fmt.Sprintf("couch-pi-%d", c.seq.Add(1))
	req, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(req, '\n')); err != nil {
		return nil, fmt.Errorf("hc: write: %w", err)
	}

	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		var resp struct {
			ID     string          `json:"id"`
			Result json.RawMessage `json:"result"`
			Error  *Error          `json:"error"`
		}
		if err := json.Unmarshal(scanner.Bytes(), &resp); err != nil {
			return nil, fmt.Errorf("hc: decode %s response: %w", method, err)
		}
		if resp.ID != id {
			continue
		}
		if resp.Error != nil {
			return nil, resp.Error
		}
		if resp.Result == nil {
			return nil, fmt.Errorf("hc: %s: empty result", method)
		}
		return resp.Result, nil
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("hc: read %s: %w", method, err)
	}
	return nil, fmt.Errorf("hc: %s: connection closed before reply", method)
}
