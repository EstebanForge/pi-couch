package server

import (
	"context"
	"hash/fnv"
	"strconv"
	"strings"
	"sync"
	"time"
)

// A scrolled editor only paints a window of its buffer. The reveal
// walks the cursor to the top - the viewport follows the cursor - then
// pages down, stitching the windows into the full message so the card
// can prefill it. Cursor keys only; no text is touched.

// revealStore caches stitched buffers keyed by a dialog fingerprint.
// The pane fragment reaches the wire a tick before the scan lands;
// the store upgrades the card the moment the full message is known.
type revealStore struct {
	mu       sync.Mutex
	stitched map[string]string
	pending  map[string]bool
}

func newRevealStore() *revealStore {
	return &revealStore{stitched: map[string]string{}, pending: map[string]bool{}}
}

func (r *revealStore) get(key string) string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.stitched[key]
}

// start claims the scan for a key; false means one is running or the
// buffer is already stitched.
func (r *revealStore) start(key string) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.pending[key] {
		return false
	}
	if _, ok := r.stitched[key]; ok {
		return false
	}
	r.pending[key] = true
	return true
}

func (r *revealStore) done(key, body string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.pending, key)
	if body != "" {
		if len(r.stitched) > 128 {
			r.stitched = map[string]string{}
		}
		r.stitched[key] = body
	}
}

func revealKey(sessionPath string, p *piDialog) string {
	h := fnv.New64a()
	h.Write([]byte(sessionPath))
	h.Write([]byte{0x1f})
	h.Write([]byte(p.Title))
	h.Write([]byte{0x1f})
	h.Write([]byte(p.Body))
	return strconv.FormatUint(h.Sum64(), 36)
}

// revealEditorBuffer reads the full buffer behind a scrolled editor.
func (a *app) revealEditorBuffer(ctx context.Context, paneID string, first *piDialog) string {
	visible := len(strings.Split(first.Body, "\n"))
	ups := make([]string, first.HiddenAbove+visible+8)
	for i := range ups {
		ups[i] = "up"
	}
	if err := a.herdr.PaneSendInput(ctx, paneID, "", ups); err != nil {
		return ""
	}
	var windows []*piDialog
	cur := a.readEditorWindow(ctx, paneID)
	for pass := 0; pass < 6 && cur != nil; pass++ {
		windows = append(windows, cur)
		if cur.HiddenBelow == 0 {
			return stitchWindows(windows)
		}
		downs := make([]string, len(strings.Split(cur.Body, "\n")))
		for i := range downs {
			downs[i] = "down"
		}
		if err := a.herdr.PaneSendInput(ctx, paneID, "", downs); err != nil {
			return ""
		}
		cur = a.readEditorWindow(ctx, paneID)
	}
	return ""
}

// readEditorWindow waits out pi's repaint, then reads the pane as an
// editor window; anything else (the dialog answered mid-scan, a pane
// that moved on) ends the scan.
func (a *app) readEditorWindow(ctx context.Context, paneID string) *piDialog {
	select {
	case <-ctx.Done():
		return nil
	case <-time.After(180 * time.Millisecond):
	}
	pr, err := a.herdr.PaneRead(ctx, paneID, "detection", 30)
	if err != nil {
		return nil
	}
	m := parsePiDialog(pr.Text)
	if m == nil || m.Kind != "editor" {
		return nil
	}
	return m
}

// stitchWindows joins viewport windows into one buffer: consecutive
// windows overlap by content, or sit exactly adjacent when the first's
// hidden-below count equals the second's hidden-above count.
func stitchWindows(ws []*piDialog) string {
	if len(ws) == 0 {
		return ""
	}
	lines := strings.Split(ws[0].Body, "\n")
	for _, w := range ws[1:] {
		wl := strings.Split(w.Body, "\n")
		k := 0
		for cand := min(len(lines), len(wl)); cand > 0; cand-- {
			if sameLines(lines[len(lines)-cand:], wl[:cand]) {
				k = cand
				break
			}
		}
		lines = append(lines, wl[k:]...)
	}
	return strings.Join(lines, "\n")
}

func sameLines(a, b []string) bool {
	for i := range a {
		if strings.TrimSpace(a[i]) != strings.TrimSpace(b[i]) {
			return false
		}
	}
	return true
}
