package push

import (
	"context"
	"log/slog"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
)

// HCWatcher is the hc driver's push surface (ADR-0008): one events
// subscription replaces the per-agent waits, and a phone buzz rides a
// turn ending or a newly blocked/error status, deduped per terminal.
// The reconcile, stability and re-arm machinery of the herdr watcher
// has no job here: hc's events are the authoritative feed.
type HCWatcher struct {
	Client  *hc.Client
	Retry   time.Duration // reconnect backoff after a lost stream
	OnEvent func(Event)
	Log     *slog.Logger

	// seen tracks the last pushed/primed state per terminal address.
	seen map[string]hcSeen
}

// hcSeen is the dedupe state for one terminal: the last turn number a
// done pushed for, and the last status state a blocked/error push (or
// a working pass) cleared.
type hcSeen struct {
	turn  int64
	state string
}

// NewHCWatcher builds the events-based watcher.
func NewHCWatcher(c *hc.Client) *HCWatcher {
	return &HCWatcher{Client: c, Retry: time.Second, seen: map[string]hcSeen{}}
}

// Run subscribes until ctx is done, re-subscribing with backoff
// whenever the stream drops. A fresh stream's snapshot primes the
// dedupe state without buzzing: old news must not replay after a
// reconnect.
func (w *HCWatcher) Run(ctx context.Context) {
	// main wires this as a struct literal: never trust the constructor
	// to have run.
	if w.seen == nil {
		w.seen = map[string]hcSeen{}
	}
	if w.Retry <= 0 {
		w.Retry = time.Second
	}
	for ctx.Err() == nil {
		stream, err := w.Client.Subscribe(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(w.Retry):
			}
			continue
		}
		for _, t := range stream.Terminals {
			if t.Status != nil {
				w.seen[t.Address] = hcSeen{state: t.Status.State}
			}
		}
		w.consume(ctx, stream)
	}
}

func (w *HCWatcher) consume(ctx context.Context, stream *hc.EventStream) {
	events := stream.Events()
	for {
		select {
		case <-ctx.Done():
			stream.Close()
			return
		case <-stream.Done():
			stream.Close()
			return
		case ev, ok := <-events:
			if !ok {
				stream.Close()
				return
			}
			w.apply(ctx, ev)
		}
	}
}

func (w *HCWatcher) apply(ctx context.Context, ev hc.Event) {
	switch ev.Type {
	case "turn":
		turn, _ := ev.Data["turn"].(float64)
		state, _ := ev.Data["state"].(string)
		s := w.seen[ev.Terminal]
		if state == "done" {
			if int64(turn) == s.turn {
				return // replay of an already-pushed turn
			}
			s.turn = int64(turn)
			s.state = "done"
			w.seen[ev.Terminal] = s
			w.buzz(ctx, ev.Terminal, "done")
		} else {
			// A running turn re-opens the dedupe window for the next
			// done, and a working status clears a parked blocked.
			s.state = state
			w.seen[ev.Terminal] = s
		}
	case "status":
		raw, _ := ev.Data["status"].(map[string]any)
		state, _ := raw["state"].(string)
		if state == "" {
			return
		}
		s := w.seen[ev.Terminal]
		if state == "blocked" || state == "error" {
			if s.state == state {
				return // parked, already buzzed
			}
			s.state = state
			w.seen[ev.Terminal] = s
			w.buzz(ctx, ev.Terminal, state)
			return
		}
		s.state = state
		w.seen[ev.Terminal] = s
	case "finished", "removed", "lost":
		delete(w.seen, ev.Terminal)
	}
}

// buzz resolves the terminal's pi.session label and pushes; a terminal
// without the label is not a Couch Pi session.
func (w *HCWatcher) buzz(ctx context.Context, addr, status string) {
	rec, err := w.Client.Get(ctx, addr)
	if err != nil {
		return
	}
	path := rec.Labels["pi.session"]
	if path == "" {
		return
	}
	if w.OnEvent != nil {
		w.OnEvent(Event{Path: path, Name: rec.Title, Status: status})
	}
}
