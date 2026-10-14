package server

import (
	"context"
	"regexp"
	"sync"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// The awareness slice: while pi sits in a blocking extension dialog
// (write review, questionnaire, settings menu, /model picker) herdr
// still reports the agent as working, so the SPA cannot tell
// "generating" from "waiting for you". herdr exposes no screen state,
// so this detector polls working agents' panes at a slow cadence and
// matches the chrome pi-tui paints around every blocking widget.
//
// pi shows one dialog at a time, so a single pattern hit is enough.

type paneSource interface {
	Agents(ctx context.Context) ([]herdr.Agent, error)
	PaneRead(ctx context.Context, paneID, source string, lines int) (herdr.PaneRead, error)
}

var (
	// Hint bars pair two key hints on one line, joined by a middle dot
	// or bullet: "Enter to select · Ctrl+S to set as default ·
	// Escape/Ctrl+C to cancel".
	hintBarRe = regexp.MustCompile(`(?i)(enter|esc(ape)?|ctrl\+[a-z])[^.·•\n]{0,40}[·•][^.·\n]{0,60}(enter|esc(ape)?|ctrl\+[a-z])`)
	// The extension editor paints a lone "ctrl+g external editor" hint.
	externalEditorRe = regexp.MustCompile(`(?i)external editor`)
	// SelectList renders its position as a standalone "(n/m)" line.
	selectCounterRe = regexp.MustCompile(`(?m)^\s{2,}\(\d+/\d+\)\s*$`)
	// ANSI styling around the chrome; strip before matching.
	ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]|\x1b\\][^\x07]*\x07")
)

// dialogOpen reports whether a pane's recent text carries dialog
// chrome. A cue, not a gate: a rare prose false positive only costs a
// harmless pill.
func dialogOpen(text string) bool {
	if text == "" {
		return false
	}
	plain := ansiRe.ReplaceAllString(text, "")
	return hintBarRe.MatchString(plain) ||
		externalEditorRe.MatchString(plain) ||
		selectCounterRe.MatchString(plain)
}

// awaitingDetector keeps a path-keyed map of agents whose pane shows
// dialog chrome. Every agent with a pane is read: the model picker and
// some extension dialogs park pi while herdr reports plain done or
// working, so the status is no filter at all - the chrome decides.
// lines=8 per pane per tick keeps the pane.read load trivial.
type awaitingDetector struct {
	hc   paneSource
	poll time.Duration

	mu       sync.Mutex
	awaiting map[string]bool
}

func newAwaitingDetector(hc paneSource, poll time.Duration) *awaitingDetector {
	return &awaitingDetector{hc: hc, poll: poll, awaiting: map[string]bool{}}
}

// run drives the detector until ctx is done.
func (d *awaitingDetector) run(ctx context.Context) {
	t := time.NewTicker(d.poll)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			d.tick(ctx)
		}
	}
}

// tick re-reads every working agent's pane and swaps the map whole, so
// readers never see a half-updated state and vanished paths prune.
func (d *awaitingDetector) tick(ctx context.Context) {
	agents, err := d.hc.Agents(ctx)
	if err != nil {
		return // dead socket: keep the last state, retry next tick
	}
	next := map[string]bool{}
	for _, a := range agents {
		if a.Session == nil || a.PaneID == "" {
			continue
		}
		pr, err := d.hc.PaneRead(ctx, a.PaneID, "recent", 8)
		if err != nil {
			continue
		}
		if dialogOpen(pr.Text) {
			next[a.Session.Value] = true
		}
	}
	d.mu.Lock()
	d.awaiting = next
	d.mu.Unlock()
}

func (d *awaitingDetector) isAwaiting(path string) bool {
	if d == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.awaiting[path]
}

// snapshot copies the map for the sessions wire; always non-nil.
func (d *awaitingDetector) snapshot() map[string]bool {
	out := map[string]bool{}
	if d == nil {
		return out
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	for k, v := range d.awaiting {
		out[k] = v
	}
	return out
}
