package push

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// herdrAPI is the slice of the herdr client the watcher needs.
type herdrAPI interface {
	Agents(ctx context.Context) ([]herdr.Agent, error)
	AgentWait(ctx context.Context, target string, until []string, timeout time.Duration) (herdr.Agent, error)
}

// Event is one stable Status transition worth pushing.
type Event struct {
	Path   string // pi session file path
	Name   string // herdr agent name
	Status string
}

// notifyStatuses are the transitions worth a phone buzz (P5 spec).
var notifyStatuses = map[string]bool{"idle": true, "blocked": true, "done": true}

// Watcher runs one agent.wait loop per live driven agent (ADR-0005). A slow
// reconcile tick spawns watchers for new agents and stops stale ones.
type Watcher struct {
	HC        herdrAPI
	Reconcile time.Duration
	Stability time.Duration // bounce window: the status must hold this long
	WaitMax   time.Duration // upper bound of one agent.wait call
	Rearm     time.Duration // pause after a matched return; herdr re-fires on agent updates while the status holds
	OnEvent   func(Event)
	Log       *slog.Logger
}

// Run blocks until ctx is done; callers wrap in a goroutine.
func (w *Watcher) Run(ctx context.Context) {
	var mu sync.Mutex
	// stop closes so idle loops exit at once; cancel kills an in-flight
	// agent.wait, which otherwise pins the goroutine and the socket
	// call for up to WaitMax after the agent vanished.
	stops := map[string]chan struct{}{}
	cancels := map[string]context.CancelFunc{}
	// notified tracks the last pushed status per session path so a late
	// herdr done->idle flip cannot double-buzz the phone.
	notified := map[string]string{}

	reconcile := func() {
		agents, err := w.HC.Agents(ctx)
		if err != nil {
			return // socket down; next tick retries
		}
		live := map[string]bool{}
		livePath := map[string]bool{}
		for _, a := range agents {
			// Any pane with a driver gets a watcher, whatever its current
			// status: a working agent is exactly the one about to finish.
			if a.PaneID == "" || a.AgentStatus == "" {
				continue
			}
			live[a.PaneID] = true
			if a.Session != nil && a.Session.Value != "" {
				livePath[a.Session.Value] = true
			}
			mu.Lock()
			// Reset the dedupe marker once the agent is back at work, so the
			// next working->done cycle pushes again. Without this, a session
			// would buzz once per distinct status until a restart.
			if a.AgentStatus == "working" && a.Session != nil && a.Session.Value != "" {
				delete(notified, a.Session.Value)
			}
			_, running := stops[a.PaneID]
			if !running {
				stop := make(chan struct{})
				stops[a.PaneID] = stop
				wctx, cancel := context.WithCancel(ctx)
				cancels[a.PaneID] = cancel
				mu.Unlock()
				go w.watch(wctx, a.PaneID, stop, &mu, notified)
			} else {
				mu.Unlock()
			}
		}
		mu.Lock()
		for pane, stop := range stops {
			if !live[pane] {
				close(stop)
				if cancel := cancels[pane]; cancel != nil {
					cancel()
					delete(cancels, pane)
				}
				delete(stops, pane)
			}
		}
		for p := range notified {
			if !livePath[p] {
				delete(notified, p)
			}
		}
		mu.Unlock()
	}

	reconcile()
	tick := time.NewTicker(w.Reconcile)
	defer tick.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			reconcile()
		}
	}
}

// watch is the per-agent loop: block on agent.wait, debounce, notify, re-arm.
// A herdr wait fires only on transitions into a state (verified, ADR-0005),
// so the loop cannot spin while the status holds.
func (w *Watcher) watch(ctx context.Context, pane string, stop <-chan struct{}, mu *sync.Mutex, notified map[string]string) {
	for {
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		default:
		}
		ag, err := w.HC.AgentWait(ctx, pane, []string{"idle", "blocked", "done", "working"}, w.WaitMax)
		if err != nil {
			select {
			case <-stop:
				return
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}
		status := ag.AgentStatus
		path := ""
		if ag.Session != nil {
			path = ag.Session.Value
		}
		// A fast turn can finish, start and finish again inside one
		// reconcile window, so the working transition itself must clear
		// the dedupe marker or the second done stays silent.
		if status == "working" && path != "" {
			mu.Lock()
			delete(notified, path)
			mu.Unlock()
			continue
		}
		if !notifyStatuses[status] || path == "" {
			continue
		}
		// Bounce window: the status must still hold after the settle delay.
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-time.After(w.Stability):
		}
		if !w.stillSame(ctx, pane, status, path) {
			continue
		}
		mu.Lock()
		// herdr re-fires the wait on every agent update while the status
		// holds; dedupe here so the phone buzzes once per cycle.
		skip := notified[path] == status || (notified[path] == "done" && status == "idle")
		if !skip {
			notified[path] = status
		}
		mu.Unlock()
		if !skip {
			w.OnEvent(Event{Path: path, Name: ag.Name, Status: status})
		}
		select {
		case <-stop:
			return
		case <-ctx.Done():
			return
		case <-time.After(w.Rearm):
		}
	}
}

// stillSame re-reads the agent list to confirm the transition held.
func (w *Watcher) stillSame(ctx context.Context, pane, status, path string) bool {
	agents, err := w.HC.Agents(ctx)
	if err != nil {
		return false
	}
	for _, a := range agents {
		if a.PaneID != pane {
			continue
		}
		return a.AgentStatus == status && a.Session != nil && a.Session.Value == path
	}
	return false
}
