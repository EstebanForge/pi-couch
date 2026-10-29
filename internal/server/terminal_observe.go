package server

import (
	"bufio"
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"strconv"
	"sync"
	"time"
)

// observeBin is the herdr binary the hub spawns; a var so tests point
// it at a fake.
var observeBin = "herdr"

type observeFrame struct {
	bytes  string // base64 ansi, straight off herdr's NDJSON
	closed bool   // the pane's stream ended
	failed bool   // the observe process could not run at all
	reset  bool   // the viewer stalled and was evicted; reconnect for a clean repaint
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
	cancel  context.CancelFunc
	mu      sync.Mutex
	subs    map[chan observeFrame]struct{}
	cols    int
	rows    int
	restart chan struct{} // cap 1: a late joiner needs herdr's frame-0 repaint
}

func (w *observeWatch) dims() (int, int) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.cols, w.rows
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
	if w := h.watches[pane]; w != nil {
		// herdr emits its full repaint only on frame 0, so a viewer
		// joining a running stream gets deltas against an empty grid:
		// respawn the process, with the new viewer's geometry winning.
		w.mu.Lock()
		w.cols, w.rows = cols, rows
		w.subs[ch] = struct{}{}
		w.mu.Unlock()
		select {
		case w.restart <- struct{}{}:
		default:
		}
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
	ctx, cancel := context.WithCancel(context.Background())
	w := &observeWatch{cancel: cancel, subs: map[chan observeFrame]struct{}{}, cols: cols, rows: rows, restart: make(chan struct{}, 1)}
	h.watches[pane] = w
	w.mu.Lock()
	w.subs[ch] = struct{}{}
	w.mu.Unlock()
	go runObserve(ctx, pane, w, h)
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
// buffer evicts it with a reset frame and a closed channel, so the
// client reconnects for a clean repaint instead of rendering a
// hole-riddled delta chain forever.
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
			w.mu.Lock()
			if _, live := w.subs[ch]; live {
				delete(w.subs, ch)
				select {
				case ch <- observeFrame{reset: true}:
				default:
				}
				close(ch)
			}
			w.mu.Unlock()
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

// runObserve keeps one herdr observe process alive per pane
// generation: a restart signal (late joiner) kills the generation so
// it respawns and frame 0's full repaint reaches everyone. EOF without
// a pending restart means the pane's stream ended.
func runObserve(ctx context.Context, pane string, w *observeWatch, h *observeHub) {
	defer w.cancel()
	for {
		if ctx.Err() != nil {
			return
		}
		cols, rows := w.dims()
		kill := make(chan struct{})
		ended := make(chan bool, 1)
		go func() { ended <- observeOnce(ctx, kill, pane, cols, rows, w, h) }()
		select {
		case closed := <-ended:
			if ctx.Err() != nil {
				return
			}
			select {
			case <-w.restart:
				continue
			case <-ctx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}
			if !closed {
				w.fan(observeFrame{closed: true})
			}
			h.drop(pane, w)
			return
		case <-w.restart:
			close(kill)
			<-ended // reap the killed generation
		case <-ctx.Done():
			close(kill)
			<-ended
			return
		}
	}
}

// observeOnce streams one process generation, fanning frames. Returns
// true when herdr itself declared the pane closed; false on EOF, error
// or kill.
func observeOnce(ctx context.Context, kill <-chan struct{}, pane string, cols, rows int, w *observeWatch, h *observeHub) bool {
	fail := func() bool {
		w.fan(observeFrame{failed: true})
		h.drop(pane, w)
		return true
	}
	bin, err := exec.LookPath(observeBin)
	if err != nil {
		return fail()
	}
	genCtx, genCancel := context.WithCancel(ctx)
	defer genCancel()
	go func() {
		select {
		case <-kill:
			genCancel()
		case <-genCtx.Done():
		}
	}()
	cmd := exec.CommandContext(genCtx, bin, "terminal", "session", "observe", pane,
		"--cols", strconv.Itoa(cols), "--rows", strconv.Itoa(rows))
	if h.socketPath != "" {
		cmd.Env = append(os.Environ(), "HERDR_SOCKET_PATH="+h.socketPath)
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return fail()
	}
	if err := cmd.Start(); err != nil {
		return fail()
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
			return true
		}
	}
	return false
}
