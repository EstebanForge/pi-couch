package hc

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
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
)

const frameHeader = 9

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
}

// Frames returns the output stream.
func (s *AttachStream) Frames() <-chan AttachFrame { return s.frames }

// Stalled reports whether the stream dropped frames on a full buffer.
func (s *AttachStream) Stalled() bool { return s.stalled.Load() }

// Close ends the stream and the connection.
func (s *AttachStream) Close() { s.closeOnce.Do(func() { _ = s.conn.Close() }) }

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
	if err := conn.SetDeadline(time.Now().Add(c.timeout)); err != nil {
		return fail(err)
	}
	hello, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0", "id": fmt.Sprintf("couch-pi-attach-%d", c.seq.Add(1)),
		"method": "hello", "params": map[string]any{"framed": true},
	})
	if _, err := conn.Write(append(hello, '\n')); err != nil {
		return fail(fmt.Errorf("hc: hello: %w", err))
	}
	br := bufio.NewReader(conn)
	line, err := br.ReadString('\n')
	if err != nil {
		return fail(fmt.Errorf("hc: hello reply: %w", err))
	}
	var hr struct {
		Error *Error `json:"error"`
	}
	if json.Unmarshal([]byte(line), &hr) != nil || hr.Error != nil {
		return fail(fmt.Errorf("hc: hello refused"))
	}
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
	// The attach reply is the next channel-0 data frame.
	for {
		channel, kind, payload, err := readFrame(br)
		if err != nil {
			return fail(fmt.Errorf("hc: attach reply: %w", err))
		}
		if channel != 0 || kind != frameData {
			continue
		}
		var resp struct {
			Result *struct {
				Channel uint32 `json:"channel"`
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
		go s.read(br, resp.Result.Channel)
		return s, nil
	}
}

// read fans the attach channel out and credits every consumed byte.
func (s *AttachStream) read(br *bufio.Reader, channel uint32) {
	defer close(s.frames)
	for {
		channelIn, kind, payload, err := readFrame(br)
		if err != nil {
			s.Close()
			return
		}
		if channelIn != channel {
			continue
		}
		switch kind {
		case frameData:
			credit := make([]byte, 4)
			binary.LittleEndian.PutUint32(credit, uint32(len(payload)))
			if _, err := s.conn.Write(encodeFrame(channel, frameCredit, credit)); err != nil {
				s.Close()
				return
			}
			select {
			case s.frames <- AttachFrame{Bytes: payload}:
			default:
				// A stalled SSE consumer: a repaint behind this point
				// would double-paint the screen, so end the stream as a
				// reset and let the client reconnect into a fresh paint.
				s.stalled.Store(true)
				s.Close()
				return
			}
		case frameClose:
			s.Close()
			return
		}
	}
}

func readFrame(br *bufio.Reader) (uint32, byte, []byte, error) {
	var head [frameHeader]byte
	if _, err := io.ReadFull(br, head[:]); err != nil {
		return 0, 0, nil, err
	}
	channel := binary.LittleEndian.Uint32(head[0:4])
	kind := head[4]
	length := binary.LittleEndian.Uint32(head[5:9])
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
// phone only reads; input rides keys.
func (d *Driver) Attach(ctx context.Context, addr string, cols, rows int) (*AttachStream, error) {
	return d.c.Attach(ctx, addr, cols, rows, true)
}
