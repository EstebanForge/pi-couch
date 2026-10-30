package hc

import (
	"context"
	"encoding/json"
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
	retry time.Duration

	mu     sync.RWMutex
	byAddr map[string]Terminal
	byPath map[string]string // pi.session path -> live terminal address
}

// NewDriver wraps a client with the session map. Run drives it.
func NewDriver(c *Client) *Driver {
	return &Driver{c: c, retry: time.Second, byAddr: map[string]Terminal{}, byPath: map[string]string{}}
}

// Run subscribes until the context ends, re-subscribing with backoff
// whenever the stream drops.
func (d *Driver) Run(ctx context.Context) {
	for ctx.Err() == nil {
		stream, err := d.c.Subscribe(ctx)
		if err == nil {
			d.reseed(stream.Terminals)
			d.consume(ctx, stream)
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
// session, so they stay out of both maps.
func (d *Driver) add(t Terminal) {
	if t.State != "live" {
		return
	}
	d.byAddr[t.Address] = t
	if path := t.Labels["pi.session"]; path != "" {
		d.byPath[path] = t.Address
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
		// A restarted keep-terminal may come back finished; add drops it.
		d.unkey(ev.Terminal)
		d.add(t)
	case "labels":
		t, ok := d.byAddr[ev.Terminal]
		if !ok {
			return
		}
		d.unkey(ev.Terminal)
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
	case "lost", "finished", "removed":
		d.unkey(ev.Terminal)
		delete(d.byAddr, ev.Terminal)
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
