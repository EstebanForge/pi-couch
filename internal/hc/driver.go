package hc

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// Driver keeps the label-driven session map (ADR-0008 wave 3): a
// pi.session-keyed view of live hc terminals. One events subscription
// feeds it, so the per-tick status quad answers from memory; a lost
// stream re-subscribes and re-seeds from the subscribe snapshot.
type Driver struct {
	c *Client

	// retry is the reconnect backoff after a lost events stream.
	// reportWait caps the wait for a terminal's first status report
	// before a Prompt (the first-paint race: an early send loses its
	// Enter); sendRetry is the gap before the started:false resend;
	// finishWait caps the graceful wait in Close; spawnReadyWait caps
	// the wait for pi's first report in Spawn. Tests shrink all of
	// them.
	retry          time.Duration
	reportWait     time.Duration
	sendRetry      time.Duration
	finishWait     time.Duration
	spawnReadyWait time.Duration

	mu     sync.RWMutex
	byAddr map[string]Terminal
	byPath map[string]string // pi.session path -> live terminal address
	lost   map[string]bool   // lost terminals: snapshots still show them live
}

// NewDriver wraps a client with the session map. Run drives it.
func NewDriver(c *Client) *Driver {
	return &Driver{c: c, retry: time.Second, reportWait: 3 * time.Second, sendRetry: time.Second, finishWait: 3 * time.Second, spawnReadyWait: 15 * time.Second, byAddr: map[string]Terminal{}, byPath: map[string]string{}, lost: map[string]bool{}}
}

// Run subscribes until the context ends, re-subscribing with backoff
// whenever the stream drops.
func (d *Driver) Run(ctx context.Context) {
	for ctx.Err() == nil {
		stream, err := d.c.Subscribe(ctx)
		if err == nil {
			d.reseed(stream.Terminals)
			d.consume(ctx, stream)
			// The stream is gone: stale badges and dead addresses must
			// not keep answering while the core is unreachable.
			d.reseed(nil)
		} else if ctx.Err() != nil {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(d.retry):
		}
	}
}

func (d *Driver) reseed(terminals []Terminal) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.byAddr = map[string]Terminal{}
	d.byPath = map[string]string{}
	for _, t := range terminals {
		d.add(t)
	}
}

// unkey drops every path pointing at the address; multiple paths can
// claim one terminal across a /new or /resume before the label event.
func (d *Driver) unkey(addr string) {
	for path, a := range d.byPath {
		if a == addr {
			delete(d.byPath, path)
		}
	}
}

// add installs one live terminal; finished records never drive a
// session, so they stay out of both maps. A lost terminal stays out
// even when a snapshot still shows it live.
func (d *Driver) add(t Terminal) {
	if t.State != "live" || d.lost[t.Address] {
		return
	}
	d.byAddr[t.Address] = t
	if path := t.Labels["pi.session"]; path != "" {
		d.byPath[path] = t.Address
	}
}

// drop removes one terminal and lets a surviving live claimant take
// its paths over (a newer terminal may have shadowed an older one).
func (d *Driver) drop(addr string) {
	var paths []string
	for path, a := range d.byPath {
		if a == addr {
			paths = append(paths, path)
		}
	}
	d.unkey(addr)
	delete(d.byAddr, addr)
	for _, path := range paths {
		for _, t := range d.byAddr {
			if t.Labels["pi.session"] == path {
				d.byPath[path] = t.Address
				break
			}
		}
	}
}

func (d *Driver) consume(ctx context.Context, stream *EventStream) {
	defer stream.Close()
	for {
		select {
		case <-ctx.Done():
			return
		case <-stream.done:
			return
		case ev, ok := <-stream.Events():
			if !ok {
				return
			}
			d.apply(ev)
		}
	}
}

// apply folds one notification into the maps, mirroring hc's event
// data shapes (viewers.rs, runtime.rs, api.rs).
func (d *Driver) apply(ev Event) {
	d.mu.Lock()
	defer d.mu.Unlock()
	switch ev.Type {
	case "created", "restarted":
		var t Terminal
		b, err := json.Marshal(ev.Data)
		if err != nil || json.Unmarshal(b, &t) != nil {
			return
		}
		delete(d.lost, ev.Terminal)
		// A restarted keep-terminal may come back finished; add drops it.
		d.unkey(ev.Terminal)
		d.add(t)
	case "labels":
		t, ok := d.byAddr[ev.Terminal]
		if !ok {
			return
		}
		d.unkey(ev.Terminal)
		// The event carries the complete label map: replace, or a
		// removed pi.session would linger forever.
		t.Labels = map[string]string{}
		if raw, ok := ev.Data["labels"]; ok && raw != nil {
			b, err := json.Marshal(raw)
			if err == nil {
				_ = json.Unmarshal(b, &t.Labels)
			}
		}
		d.add(t)
	case "status":
		t, ok := d.byAddr[ev.Terminal]
		if !ok {
			return
		}
		raw, ok := ev.Data["status"]
		if !ok || raw == nil {
			t.Status = nil
		} else if b, err := json.Marshal(raw); err == nil {
			var s Status
			if json.Unmarshal(b, &s) == nil {
				t.Status = &s
			}
		}
		if rep, ok := ev.Data["reported"].(bool); ok {
			t.Reported = rep
		}
		d.byAddr[ev.Terminal] = t
	case "turn":
		t, ok := d.byAddr[ev.Terminal]
		if !ok {
			return
		}
		if turn, ok := ev.Data["turn"].(float64); ok {
			t.Turn = int64(turn)
		}
		if state, ok := ev.Data["state"].(string); ok && state != "" {
			if t.Status == nil {
				t.Status = &Status{}
			}
			t.Status.State = state
		}
		d.byAddr[ev.Terminal] = t
	case "title":
		if t, ok := d.byAddr[ev.Terminal]; ok {
			if title, ok := ev.Data["title"].(string); ok {
				t.Title = title
				d.byAddr[ev.Terminal] = t
			}
		}
	case "lost":
		d.lost[ev.Terminal] = true
		d.drop(ev.Terminal)
	case "finished", "removed":
		delete(d.lost, ev.Terminal)
		d.drop(ev.Terminal)
	}
}

// Badges maps pi session file paths to status states. Degrades to an
// empty map like herdr's reader.
func (d *Driver) Badges(_ context.Context) map[string]string {
	badges, _ := d.DriveStatus(context.Background())
	return badges
}

// Driven maps pi session file paths to the live terminal driving them.
func (d *Driver) Driven(_ context.Context) map[string]herdr.DrivenInfo {
	_, driven := d.DriveStatus(context.Background())
	return driven
}

// DriveStatus joins the badge and driven maps from one map pass.
func (d *Driver) DriveStatus(_ context.Context) (map[string]string, map[string]herdr.DrivenInfo) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	badges := map[string]string{}
	driven := map[string]herdr.DrivenInfo{}
	for path, addr := range d.byPath {
		t, ok := d.byAddr[addr]
		if !ok || t.Status == nil || t.Status.State == "" {
			continue
		}
		badges[path] = t.Status.State
		driven[path] = herdr.DrivenInfo{Status: t.Status.State, Title: t.Title}
	}
	return badges, driven
}

// TargetForPath returns the terminal address driving the session file.
func (d *Driver) TargetForPath(_ context.Context, path string) (string, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	addr, ok := d.byPath[path]
	if !ok {
		return "", herdr.ErrNotDriven
	}
	return addr, nil
}

// SetRetryForTest shrinks the reconnect backoff for tests.
func (d *Driver) SetRetryForTest(retry time.Duration) { d.retry = retry }

// Agents renders the labeled live terminals as herdr agents, the shape
// the awaiting detector polls. Status rides the terminal's own OSC
// 7501 report, so hc blocks surface here without any regex.
func (d *Driver) Agents(_ context.Context) ([]herdr.Agent, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	agents := make([]herdr.Agent, 0, len(d.byPath))
	for path, addr := range d.byPath {
		t := d.byAddr[addr]
		st := ""
		if t.Status != nil {
			st = t.Status.State
		}
		agents = append(agents, herdr.Agent{
			Name:          t.Title,
			AgentStatus:   st,
			PaneID:        addr,
			TerminalTitle: t.Title,
			Session:       &herdr.AgentSessionRef{Kind: "path", Value: path},
		})
	}
	return agents, nil
}

// PaneRead maps hc's screen read onto herdr's read shape so the
// detector and dialog parse run unchanged over both drivers. hc
// headless terminals run 200x120, double the line budget the herdr
// detection source needs.
func (d *Driver) PaneRead(ctx context.Context, addr, _ string, lines int) (herdr.PaneRead, error) {
	if lines < 30 {
		lines = 30
	}
	res, err := d.c.Read(ctx, addr, lines*2)
	if err != nil {
		return herdr.PaneRead{}, err
	}
	return herdr.PaneRead{PaneID: addr, Text: res.Text}, nil
}

// ReadScreen returns the terminal's current screen text for the dialog
// answer path's verify re-reads.
func (d *Driver) ReadScreen(ctx context.Context, addr string, lines int) (string, error) {
	res, err := d.c.Read(ctx, addr, lines)
	if err != nil {
		return "", err
	}
	return res.Text, nil
}

// PressKeys presses named keys on the terminal (enter, esc, down, ...).
func (d *Driver) PressKeys(ctx context.Context, addr string, keys []string) error {
	return d.c.Keys(ctx, addr, keys)
}

// TypeText pastes text and Enter in one step, forcing through a
// blocked status: the answer path has already verified the dialog is
// the one the request targeted.
func (d *Driver) TypeText(ctx context.Context, addr, text string) error {
	_, err := d.c.Send(ctx, addr, text, SendParams{Force: true})
	return err
}

// reported waits until the terminal has filed its first status report
// (bounded), so a Prompt never lands in the first-paint composer.
func (d *Driver) reported(addr string) {
	deadline := time.Now().Add(d.reportWait)
	for {
		d.mu.RLock()
		t, ok := d.byAddr[addr]
		d.mu.RUnlock()
		if ok && t.Reported {
			return
		}
		if time.Now().After(deadline) {
			return
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Prompt submits one user prompt to the terminal. hc still owns its
// quiet-and-settle pacing, so callers budget the context generously.
func (d *Driver) Prompt(ctx context.Context, addr, text string) error {
	d.reported(addr)
	res, err := d.c.Send(ctx, addr, text, SendParams{})
	if err != nil {
		return err
	}
	if res.Started != nil && !*res.Started {
		// started:false means the turn did not begin in the send
		// window. The first-paint race leaves the draft pasted in the
		// composer with its Enter lost, so resending would double the
		// text: nudge Enter instead, same as the create flow.
		select {
		case <-time.After(d.sendRetry):
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := d.c.Keys(ctx, addr, []string{"enter"}); err != nil {
			return err
		}
	}
	return nil
}

// Stop aborts the terminal's current turn: hc presses esc for
// status-reporting programs, which is the correct pi interrupt.
func (d *Driver) Stop(ctx context.Context, addr string) error {
	_, err := d.c.Interrupt(ctx, addr, 8*time.Second)
	return err
}

// Spawn runs pi in a fresh terminal and waits for its first status
// report - the moment the TUI reads input. The line (never argv: a
// template named "pi" would hijack the spawn) is built by shell
// quoting each argument.
func (d *Driver) Spawn(ctx context.Context, dir, name string, argv []string) (Terminal, error) {
	parts := make([]string, 0, len(argv)+1)
	parts = append(parts, "pi")
	for _, a := range argv {
		parts = append(parts, shQuote(a))
	}
	var t Terminal
	res, err := d.c.Run(ctx, RunParams{Name: name, Cwd: dir, Line: strings.Join(parts, " ")})
	if err != nil {
		return t, err
	}
	deadline := time.Now().Add(d.spawnReadyWait)
	fail := func(err error) (Terminal, error) {
		// A failed spawn must not leak a live pi for a name retry to
		// shadow. Clean up on a fresh context: the caller's may be done.
		cctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		d.Discard(cctx, res.Address)
		return Terminal{}, err
	}
	for {
		d.mu.RLock()
		rec, ok := d.byAddr[res.Address]
		reported := ok && rec.Reported
		d.mu.RUnlock()
		if reported {
			return res, nil
		}
		if time.Now().After(deadline) {
			return fail(errors.New("hc: pi never filed its first status report"))
		}
		select {
		case <-ctx.Done():
			return fail(ctx.Err())
		case <-time.After(25 * time.Millisecond):
		}
	}
}

// SessionLabel returns the terminal's pi.session label, if the bridge
// has filed one yet.
func (d *Driver) SessionLabel(addr string) (string, bool) {
	d.mu.RLock()
	defer d.mu.RUnlock()
	t, ok := d.byAddr[addr]
	if !ok {
		return "", false
	}
	p, ok := t.Labels["pi.session"]
	return p, ok && p != ""
}

// Discard tears down a spawn that failed before it became a session:
// kill (a no-op on a non-live terminal) then remove, best-effort.
func (d *Driver) Discard(ctx context.Context, addr string) error {
	if err := d.c.Kill(ctx, addr); err != nil {
		return err
	}
	_ = d.c.Remove(ctx, addr)
	return nil
}

// finished polls until the terminal leaves the live set (or up to
// finishWait), reporting whether it exited.
func (d *Driver) finished(addr string) bool {
	deadline := time.Now().Add(d.finishWait)
	for {
		d.mu.RLock()
		_, ok := d.byAddr[addr]
		d.mu.RUnlock()
		if !ok {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		time.Sleep(25 * time.Millisecond)
	}
}

// Close ends the session's pi: graceful ctrl+d first, kill as the
// escalation (herdr's close ran the same dance through the pane), then
// the record removal best-effort. The jsonl stays as an archive.
func (d *Driver) Close(ctx context.Context, addr string) error {
	keyCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	err := d.c.Keys(keyCtx, addr, []string{"ctrl+d"})
	cancel()
	_ = err // an un-quiet screen only moves us to the escalation sooner
	if !d.finished(addr) {
		if err := d.c.Kill(ctx, addr); err != nil {
			return err
		}
	}
	_ = d.c.Remove(ctx, addr)
	return nil
}

// shQuote single-quotes one shell word, escaping embedded quotes.
func shQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
