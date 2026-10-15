package server

import (
	"context"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// The awareness slice: while pi sits in a blocking extension dialog
// (write review, questionnaire, settings menu, /model picker) herdr
// still reports the agent as working or done, so the SPA cannot tell
// "generating" from "waiting for you". herdr exposes no dialog-open
// state (its detection screen is a read surface, not an event), and pi
// never reports blocked for a picker, so this detector polls agents'
// panes at a slow cadence and matches the chrome pi-tui paints around
// every blocking widget.
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
	// pi's own dialog hints are space-separated, with no middle dot, so
	// hintBarRe never matches them (measured on pi 1.2, same strings
	// herdr-web-ui pins in prompt.ts). The select/confirm menu reads
	// "↑↓ navigate  enter select  escape/ctrl+c cancel"; the input line
	// reads "enter submit  escape/ctrl+c cancel".
	piMenuHintRe  = regexp.MustCompile(`(?i)\x{2191}\x{2193}\s+navigate\s+enter\s+select\s+escape/ctrl\+c\s+cancel`)
	piInputHintRe = regexp.MustCompile(`(?i)enter\s+submit\s+escape/ctrl\+c\s+cancel`)
	// The extension editor paints a lone "ctrl+g external editor" hint.
	externalEditorRe = regexp.MustCompile(`(?i)external editor`)
	// SelectList renders its position as a standalone "(n/m)" line.
	selectCounterRe = regexp.MustCompile(`(?m)^\s{2,}\(\d+/\d+\)\s*$`)
	// ANSI styling around the chrome; strip before matching.
	ansiRe = regexp.MustCompile("\x1b\\[[0-9;?]*[a-zA-Z]|\x1b\\][^\x07]*\x07")
)

// piHintAtEnd matches pi's hint discipline (their hintAtEnd): the hint
// must sit at the END of the pane text, with at most a few lines of
// pi's own footer (folder, context meter) below it. A dialog already
// answered leaves its hint far above later content, which is what an
// unanchored match would wrongly keep offering.
func piHintAtEnd(plain string) bool {
	lines := strings.Split(plain, "\n")
	// Trim trailing empties first, so blank padding cannot push the hint
	// out of the window.
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	lines = lines[:end]
	// The hint may sit a few rows above the bottom (pi paints its footer
	// under every dialog), and a phone-width pane can wrap it, so test
	// growing joins of the trailing lines.
	const window = 6
	start := end - window
	if start < 0 {
		start = 0
	}
	for i := start; i < end; i++ {
		joined := strings.Join(lines[i:], " ")
		if piMenuHintRe.MatchString(joined) || piInputHintRe.MatchString(joined) {
			return true
		}
	}
	return false
}

// dialogOpen reports whether a pane's recent text carries dialog
// chrome. A cue, not a gate: a rare prose false positive only costs a
// harmless pill.
func dialogOpen(text string) bool {
	if text == "" {
		return false
	}
	plain := ansiRe.ReplaceAllString(text, "")
	return hintBarRe.MatchString(plain) ||
		piHintAtEnd(plain) ||
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
