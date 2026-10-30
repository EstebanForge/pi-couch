package hc

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"net"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// framedFake speaks the wire protocol: plain JSON lines until a hello
// with framed:true, then length-prefixed frames. It answers one
// terminal.attach with channel 9, paints, streams deltas, records
// credits, then closes the attach channel when closedSeen fires.
type framedFake struct {
	t    *testing.T
	sock string

	paint  []byte
	deltas [][]byte
	finish bool // reply to attach with a JSON-RPC error instead

	mu       sync.Mutex
	credits  []int
	attached map[string]any
}

func startFramedFake(t *testing.T, paint []byte, deltas [][]byte, finish bool) *framedFake {
	t.Helper()
	f := &framedFake{t: t, paint: paint, deltas: deltas, finish: finish, attached: map[string]any{}}
	dir := t.TempDir()
	sockPath := filepath.Join(dir, "hc.sock")
	ln, err := net.Listen("unix", sockPath)
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
			go f.serve(conn)
		}
	}()
	f.sock = sockPath
	return f
}

func (f *framedFake) serve(conn net.Conn) {
	defer conn.Close()
	br := bufio.NewReader(conn)
	hello, err := br.ReadString('\n')
	if err != nil {
		return
	}
	var req struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Framed bool `json:"framed"`
		} `json:"params"`
	}
	if json.Unmarshal([]byte(hello), &req) != nil || req.Method != "hello" || !req.Params.Framed {
		return
	}
	reply, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": req.ID, "result": map[string]any{"generation": 1, "framed": true}})
	if _, err := conn.Write(append(reply, '\n')); err != nil {
		return
	}

	for {
		head := make([]byte, 9)
		if _, err := ioReadFull(br, head); err != nil {
			return
		}
		channel := binary.LittleEndian.Uint32(head[0:4])
		kind := head[4]
		length := binary.LittleEndian.Uint32(head[5:9])
		payload := make([]byte, length)
		if _, err := ioReadFull(br, payload); err != nil {
			return
		}
		if channel == 0 && kind == 0 {
			var call struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Params map[string]any  `json:"params"`
			}
			if json.Unmarshal(payload, &call) != nil || call.Method != "terminal.attach" {
				continue
			}
			f.mu.Lock()
			f.attached = call.Params
			finish := f.finish
			f.mu.Unlock()
			if finish {
				out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "error": map[string]any{"code": -32000, "message": "finished", "data": map[string]any{"kind": "finished"}}})
				_, _ = conn.Write(encodeFrame(0, 0, out))
				return
			}
			out, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": call.ID, "result": map[string]any{"channel": 9, "terminal": map[string]any{"address": "t1", "state": "live"}}})
			if _, err := conn.Write(encodeFrame(0, 0, out)); err != nil {
				return
			}
			if len(f.paint) > 0 {
				if _, err := conn.Write(encodeFrame(9, 0, f.paint)); err != nil {
					return
				}
			}
			for _, d := range f.deltas {
				if _, err := conn.Write(encodeFrame(9, 0, d)); err != nil {
					return
				}
			}
			if _, err := conn.Write(encodeFrame(9, 2, []byte{0})); err != nil {
				return
			}
			continue
		}
		if channel == 9 && kind == 1 && len(payload) == 4 {
			f.mu.Lock()
			f.credits = append(f.credits, int(binary.LittleEndian.Uint32(payload)))
			f.mu.Unlock()
		}
	}
}

func (f *framedFake) creditTotal() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	n := 0
	for _, c := range f.credits {
		n += c
	}
	return n
}

func (f *framedFake) attachParam(key string) any {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.attached[key]
}

// ioReadFull mirrors io.ReadFull over a bufio.Reader.
func ioReadFull(br *bufio.Reader, buf []byte) (int, error) {
	n := 0
	for n < len(buf) {
		m, err := br.Read(buf[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func TestAttachStreamsPaintDeltasAndCredit(t *testing.T) {
	f := startFramedFake(t, []byte("first paint"), [][]byte{[]byte("more"), []byte("bytes")}, false)
	cl := NewAt(f.sock)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := cl.Attach(ctx, "t1", 100, 30, true)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	defer stream.Close()

	var got [][]byte
	deadline := time.After(2 * time.Second)
	for {
		select {
		case fr, ok := <-stream.Frames():
			if !ok {
				// The fake closed the attach channel: streaming done.
				if string(got[0]) != "first paint" || len(got) != 3 {
					t.Fatalf("frames = %v, want paint plus two deltas", got)
				}
				waitFor(t, func() bool {
					return f.creditTotal() == len("first paint")+len("more")+len("bytes")
				})
				if f.attachParam("terminal") != "t1" {
					t.Fatalf("attach terminal = %v, want t1", f.attachParam("terminal"))
				}
				if f.attachParam("observe") != true {
					t.Fatalf("attach observe = %v, want true", f.attachParam("observe"))
				}
				return
			}
			got = append(got, fr.Bytes)
		case <-deadline:
			t.Fatal("attach stream never closed")
		}
	}
}

func TestAttachRefusesAFinishedTerminal(t *testing.T) {
	f := startFramedFake(t, nil, nil, true)
	cl := NewAt(f.sock)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if _, err := cl.Attach(ctx, "t1", 100, 30, true); err == nil {
		t.Fatal("attach on a finished terminal must fail")
	}
}

func TestAttachCloseEndsTheStream(t *testing.T) {
	// A core that never closes: the client's Close must end the read.
	f := startFramedFake(t, []byte("paint"), nil, false)
	cl := NewAt(f.sock)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := cl.Attach(ctx, "t1", 100, 30, true)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	fr := <-stream.Frames()
	if string(fr.Bytes) != "paint" {
		t.Fatalf("first frame = %q, want the paint", fr.Bytes)
	}
	stream.Close()
	select {
	case _, ok := <-stream.Frames():
		if ok {
			t.Fatal("frames must close after Close")
		}
	case <-time.After(time.Second):
		t.Fatal("Close did not end the stream")
	}
}

func TestAttachStallsOutOnAnUnreadStream(t *testing.T) {
	// More deltas than the frame buffer holds, none consumed: the
	// stream must mark itself stalled and end.
	paint := []byte("paint")
	deltas := make([][]byte, 200)
	for i := range deltas {
		deltas[i] = []byte("x")
	}
	f := startFramedFake(t, paint, deltas, false)
	cl := NewAt(f.sock)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	stream, err := cl.Attach(ctx, "t1", 100, 30, true)
	if err != nil {
		t.Fatalf("attach: %v", err)
	}
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("stalled stream never ended")
		default:
		}
		if stream.Stalled() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
}
