package server

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"sync"
)

// observeBin is the herdr binary the hub spawns; a var so tests point
// it at a fake.
var observeBin = "herdr"

type observeFrame struct {
	bytes  string // base64 ansi, straight off herdr's NDJSON
	closed bool   // the pane's stream ended
	failed bool   // the observe process could not run at all
}

// observeHub fans one `herdr terminal session observe` subprocess per
// pane out to every terminal viewer of that pane. The process is a
// read-only viewport: it never locks the pane grid or owns resize, so
// the desk CLI's exclusive attach stays untouched.
type observeHub struct {
	mu         sync.Mutex
	watches    map[string]*observeWatch
	socketPath string // herdr's unix socket, passed to the subprocess
}

type observeWatch struct {
	cancel context.CancelFunc
	mu     sync.Mutex
	subs   map[chan observeFrame]struct{}
}

func newObserveHub(socketPath string) *observeHub {
	return &observeHub{watches: map[string]*observeWatch{}, socketPath: socketPath}
}

const observeSubBuffer = 64

func (h *observeHub) subscribe(pane string, cols, rows int) (<-chan observeFrame, func()) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.watches == nil {
		h.watches = map[string]*observeWatch{}
	}
	ch := make(chan observeFrame, observeSubBuffer)
	w, ok := h.watches[pane]
	if !ok {
		ctx, cancel := context.WithCancel(context.Background())
		w = &observeWatch{cancel: cancel, subs: map[chan observeFrame]struct{}{}}
		h.watches[pane] = w
		go runObserve(ctx, pane, cols, rows, w, h)
	}
	w.mu.Lock()
	w.subs[ch] = struct{}{}
	w.mu.Unlock()
	return ch, func() {
		w.mu.Lock()
		_, live := w.subs[ch]
		delete(w.subs, ch)
		w.mu.Unlock()
		if !live {
			return
		}
		h.mu.Lock()
		defer h.mu.Unlock()
		cur, ok := h.watches[pane]
		if !ok || cur != w || len(w.subs) > 0 {
			return
		}
		w.cancel()
		delete(h.watches, pane)
	}
}

// fan delivers one frame to every subscriber; a stalled viewer's full
// buffer drops frames instead of wedging the hub (the sessions-stream
// drop-on-full precedent). Sends happen outside the lock so a slow
// consumer never blocks subscribe.
func (w *observeWatch) fan(f observeFrame) {
	w.mu.Lock()
	subs := make([]chan observeFrame, 0, len(w.subs))
	for ch := range w.subs {
		subs = append(subs, ch)
	}
	w.mu.Unlock()
	for _, ch := range subs {
		select {
		case ch <- f:
		default:
		}
	}
}

func (h *observeHub) drop(pane string, w *observeWatch) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if cur, ok := h.watches[pane]; ok && cur == w {
		delete(h.watches, pane)
	}
}

// runObserve streams herdr's NDJSON viewport: terminal.frame records
// carry base64 ansi pty bytes (the first record is a full repaint), and
// terminal.closed or stdout EOF means the pane's stream ended.
func runObserve(ctx context.Context, pane string, cols, rows int, w *observeWatch, h *observeHub) {
	defer w.cancel()
	fail := func() {
		w.fan(observeFrame{failed: true})
		h.drop(pane, w)
	}
	bin, err := exec.LookPath(observeBin)
	if err != nil {
		fail()
		return
	}
	cmd := exec.CommandContext(ctx, bin, "terminal", "session", "observe", pane,
		"--cols", strconv.Itoa(cols), "--rows", strconv.Itoa(rows))
	if h.socketPath != "" {
		cmd.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+h.socketPath)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		fail()
		return
	}
	if err := cmd.Start(); err != nil {
		fail()
		return
	}
	defer func() { _ = cmd.Wait() }()

	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 0, 64<<10), 1<<20)
	var rec struct {
		Type     string `json:"type"`
		Encoding string `json:"encoding"`
		Bytes    string `json:"bytes"`
	}
	for sc.Scan() {
		rec.Type, rec.Encoding, rec.Bytes = "", "", ""
		if json.Unmarshal(sc.Bytes(), &rec) != nil {
			continue
		}
		if rec.Type == "terminal.frame" && rec.Encoding == "ansi" && rec.Bytes != "" {
			w.fan(observeFrame{bytes: rec.Bytes})
		} else if rec.Type == "terminal.closed" {
			w.fan(observeFrame{closed: true})
		}
	}
	w.fan(observeFrame{closed: true})
	h.drop(pane, w)
}
