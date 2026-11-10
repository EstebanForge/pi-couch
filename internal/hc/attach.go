package hc

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"time"
)

// Wire frame kinds (herdr-core wire.rs).
const (
	frameData   = 0
	frameCredit = 1
	frameClose  = 2
	// frameAutomaticInput is hc 0.1.19's AUTOMATIC_INPUT frame: raw
	// bytes into the pty with no Enter appended - the paste verb the
	// keys vocabulary cannot express. Writer attaches only; observe
	// viewers silently drop it.
	frameAutomaticInput = 6
)

const frameHeader = 9

// maxFramePayload mirrors the core's own 16 MiB wire cap.
const maxFramePayload = 16 << 20

type attachPending struct {
	channel uint32
	kind    byte
	payload []byte
}

// AttachFrame is one chunk of terminal output.
type AttachFrame struct {
	Bytes []byte
}

// AttachStream is a live terminal byte stream. Frames closes when
// the terminal's output ends or Close is called; Stalled reports
// that frames were dropped because the consumer stopped reading, so
// the stream must end as a reset (the client reconnects into a fresh
// full paint) rather than as a clean end.
type AttachStream struct {
	frames    chan AttachFrame
	conn      net.Conn
	closeOnce sync.Once
	stalled   atomic.Bool
	// Cols and Rows are the core's real grid, not the requested one:
	// observe viewers never set the terminal size.
	Cols int
	Rows int
}

// Frames returns the output stream.
func (s *AttachStream) Frames() <-chan AttachFrame { return s.frames }

// Stalled reports whether the stream dropped frames on a full buffer.
func (s *AttachStream) Stalled() bool { return s.stalled.Load() }

// Close ends the stream and the connection.
func (s *AttachStream) Close() { s.closeOnce.Do(func() { _ = s.conn.Close() }) }

// helloFramed upgrades a fresh connection to framed mode. Both Attach
// and Write run it before their attach call.
func (c *Client) helloFramed(ctx context.Context, conn net.Conn) error {
	if err := conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return err
	}
	hello, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": fmt.Sprintf("couch-pi-attach-%d", c.seq.Add(1)),
		"method": "hello", "params": map[string]any{"framed": true},
	})
	if _, err := conn.Write(append(hello, '\n')); err != nil {
		return fmt.Errorf("hc: hello: %w", err)
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return fmt.Errorf("hc: hello reply: %w", err)
	}
	var hr struct {
		Error *Error `json:"error"`
	}
	if json.Unmarshal([]byte(line), &hr) != nil || hr.Error != nil {
		return fmt.Errorf("hc: hello refused")
	}
	return nil
}

// Write injects raw bytes into the terminal's pty without appending
// Enter - the paste verb hc's Send lacks. Every call is its own
// short-lived writer attach: the reply must advertise
// automatic_input (the core silently drops kind 6 on observe
// attaches), cols/rows stay zero so the core keeps its grid, and the
// connection drains-and-credits until quiet before closing.
// ErrNoAutomaticInput marks a core whose attach reply lacks
// automatic_input: kind-6 frames would be dropped silently, so callers
// get a permanent refusal instead of a fake success.
var ErrNoAutomaticInput = errors.New("hc: attach has no automatic_input")

func (c *Client) Write(ctx context.Context, addr string, data []byte) error {
	conn, err := c.dial(ctx)
	if err != nil {
		return err
	}
	defer conn.Close()
	fail := func(e error) error {
		_ = conn.Close()
		return e
	}
	if err := c.helloFramed(ctx, conn); err != nil {
		return fail(err)
	}
	call, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "write",
		"method": "terminal.attach",
		"params": map[string]any{"terminal": addr, "observe": false, "cols": 0, "rows": 0},
	})
	if _, err := conn.Write(encodeFrame(0, frameData, call)); err != nil {
		return fail(fmt.Errorf("hc: write attach: %w", err))
	}
	br := bufio.NewReader(conn)
	var reply struct {
		Result *struct {
			Channel        uint32 `json:"channel"`
			AutomaticInput bool   `json:"automatic_input"`
		} `json:"result"`
		Error *Error `json:"error"`
	}
	for {
		ch, kind, payload, err := readFrame(br)
		if err != nil {
			return fail(fmt.Errorf("hc: write attach reply: %w", err))
		}
		if kind != frameData {
			continue
		}
		if ch != 0 {
			// Output can overtake the reply on the stream channel;
			// consumed bytes must be credited or the core stalls its
			// 256 KiB window under a burst of writes.
			credit := make([]byte, 4)
			binary.LittleEndian.PutUint32(credit, uint32(len(payload)))
			if _, err := conn.Write(encodeFrame(ch, frameCredit, credit)); err != nil {
				return fail(fmt.Errorf("hc: write credit: %w", err))
			}
			continue
		}
		if json.Unmarshal(payload, &reply) != nil {
			return fail(fmt.Errorf("hc: write attach: bad reply"))
		}
		break
	}
	if reply.Error != nil {
		return fail(reply.Error)
	}
	if reply.Result == nil || !reply.Result.AutomaticInput {
		// Fail closed: a core without AUTOMATIC_INPUT drops kind 6
		// silently, and typing into the void looks like success.
		return fail(ErrNoAutomaticInput)
	}
	ch := reply.Result.Channel
	if _, err := conn.Write(encodeFrame(ch, frameAutomaticInput, data)); err != nil {
		return fail(fmt.Errorf("hc: write: %w", err))
	}
	// Drain and credit on a fixed 250 ms deadline so the core never
	// stalls its 256 KiB output window mid-write; the output itself is
	// not ours to keep. Under a heavy echo the write abandons the
	// stream and closing the connection frees the window.
	conn.SetReadDeadline(time.Now().Add(250 * time.Millisecond))
	credit := make([]byte, 4)
	for {
		_, kind, payload, err := readFrame(br)
		if err != nil {
			break
		}
		if kind == frameClose {
			break
		}
		if kind == frameData && len(payload) > 0 {
			binary.LittleEndian.PutUint32(credit, uint32(len(payload)))
			if _, err := conn.Write(encodeFrame(ch, frameCredit, credit)); err != nil {
				break
			}
		}
	}
	return nil
}

// Attach opens a raw output stream to a terminal. The connection is
// framed mode: a hello with framed:true upgrades the line protocol,
// terminal.attach answers with the stream's channel, and every
// consumed byte must be credited back or the core stops at its
// 256 KiB window. Observe viewers only read; they never set the size.
func (c *Client) Attach(ctx context.Context, addr string, cols, rows int, observe bool) (*AttachStream, error) {
	conn, err := c.dial(ctx)
	if err != nil {
		return nil, err
	}
	s := &AttachStream{frames: make(chan AttachFrame, 64), conn: conn}
	fail := func(e error) (*AttachStream, error) {
		_ = conn.Close()
		return nil, e
	}
	if err := c.helloFramed(ctx, conn); err != nil {
		return fail(err)
	}
	br := bufio.NewReader(conn)
	call, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": "attach",
		"method": "terminal.attach",
		"params": map[string]any{"terminal": addr, "observe": observe, "cols": cols, "rows": rows},
	})
	if _, err := conn.Write(encodeFrame(0, frameData, call)); err != nil {
		return fail(fmt.Errorf("hc: attach: %w", err))
	}
	if err := conn.SetDeadline(time.Time{}); err != nil {
		return fail(err)
	}
	// The reply stays bounded and cancellable; only the live stream
	// after it is unbounded.
	stop := context.AfterFunc(ctx, func() { _ = conn.SetDeadline(time.Now()) })
	defer stop()
	// The core queues the first paint BEFORE the attach reply. Keep
	// those frames and replay them, or the screen starts blank.
	var early []attachPending
	for {
		channel, kind, payload, err := readFrame(br)
		if err != nil {
			return fail(fmt.Errorf("hc: attach reply: %w", err))
		}
		if channel != 0 || kind != frameData {
			early = append(early, attachPending{channel, kind, payload})
			continue
		}
		var resp struct {
			Result *struct {
				Channel  uint32   `json:"channel"`
				Terminal Terminal `json:"terminal"`
			} `json:"result"`
			Error *Error `json:"error"`
		}
		if json.Unmarshal(payload, &resp) != nil {
			return fail(fmt.Errorf("hc: bad attach reply"))
		}
		if resp.Error != nil {
			return fail(resp.Error)
		}
		if resp.Result == nil || resp.Result.Channel == 0 {
			return fail(fmt.Errorf("hc: empty attach result"))
		}
		s.Cols, s.Rows = resp.Result.Terminal.Cols, resp.Result.Terminal.Rows
		if err := conn.SetDeadline(time.Time{}); err != nil {
			return fail(err)
		}
		go s.read(br, resp.Result.Channel, early)
		return s, nil
	}
}

// read fans the attach channel out and credits every consumed byte.
func (s *AttachStream) read(br *bufio.Reader, channel uint32, early []attachPending) {
	defer close(s.frames)
	for _, f := range early {
		if !s.deliver(channel, f.channel, f.kind, f.payload) {
			return
		}
	}
	for {
		channelIn, kind, payload, err := readFrame(br)
		if err != nil {
			s.Close()
			return
		}
		if !s.deliver(channel, channelIn, kind, payload) {
			return
		}
	}
}

// deliver handles one frame; false ends the stream.
func (s *AttachStream) deliver(channel, channelIn uint32, kind byte, payload []byte) bool {
	if channelIn != channel {
		return true
	}
	switch kind {
	case frameData:
		credit := make([]byte, 4)
		binary.LittleEndian.PutUint32(credit, uint32(len(payload)))
		if _, err := s.conn.Write(encodeFrame(channel, frameCredit, credit)); err != nil {
			s.Close()
			return false
		}
		select {
		case s.frames <- AttachFrame{Bytes: payload}:
		default:
			// A stalled SSE consumer: a repaint behind this point
			// would double-paint the screen, so end the stream as a
			// reset and let the client reconnect into a fresh paint.
			s.stalled.Store(true)
			s.Close()
			return false
		}
	case frameClose:
		s.Close()
		return false
	}
	return true
}

func readFrame(br *bufio.Reader) (uint32, byte, []byte, error) {
	var head [frameHeader]byte
	if _, err := io.ReadFull(br, head[:]); err != nil {
		return 0, 0, nil, err
	}
	channel := binary.LittleEndian.Uint32(head[0:4])
	kind := head[4]
	length := binary.LittleEndian.Uint32(head[5:9])
	if length > maxFramePayload {
		return 0, 0, nil, fmt.Errorf("hc: frame of %d bytes exceeds the wire cap", length)
	}
	payload := make([]byte, length)
	if _, err := io.ReadFull(br, payload); err != nil {
		return 0, 0, nil, err
	}
	return channel, kind, payload, nil
}

func encodeFrame(channel uint32, kind byte, payload []byte) []byte {
	out := make([]byte, frameHeader+len(payload))
	binary.LittleEndian.PutUint32(out[0:4], channel)
	out[4] = kind
	binary.LittleEndian.PutUint32(out[5:9], uint32(len(payload)))
	copy(out[frameHeader:], payload)
	return out
}

// Attach wraps the client's framed attach as an observe viewer: the
// phone only reads; input rides keys and the writer attach (ADR-0012).
func (d *Driver) Attach(ctx context.Context, addr string, cols, rows int) (*AttachStream, error) {
	return d.c.Attach(ctx, addr, cols, rows, true)
}
