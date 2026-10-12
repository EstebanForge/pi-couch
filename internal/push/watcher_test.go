package push

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// fakeHerdr scripts AgentWait results per pane in order; the last step of a
// pane repeats. Each step may mutate the agent list after firing, which
// models herdr's own state converging on the transition.
type fakeHerdr struct {
	mu     sync.Mutex
	agents []herdr.Agent
	script map[string][]waitStep
}

type waitStep struct {
	ag   herdr.Agent
	err  error
	then func(*fakeHerdr)
}

func (f *fakeHerdr) Agents(context.Context) ([]herdr.Agent, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	out := make([]herdr.Agent, len(f.agents))
	copy(out, f.agents)
	return out, nil
}

func (f *fakeHerdr) AgentWait(_ context.Context, target string, _ []string, _ time.Duration) (herdr.Agent, error) {
	f.mu.Lock()
	var step waitStep
	steps := f.script[target]
	switch {
	case len(steps) > 1:
		step, f.script[target] = steps[0], steps[1:]
	case len(steps) == 1:
		step = steps[0]
	default:
		f.mu.Unlock()
		time.Sleep(20 * time.Millisecond)
		return herdr.Agent{}, context.DeadlineExceeded
	}
	f.mu.Unlock()
	if step.then != nil {
		step.then(f)
	}
	return step.ag, step.err
}

func (f *fakeHerdr) setAgents(as ...herdr.Agent) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.agents = as
}

func probeAgent(status string, withSession bool) herdr.Agent {
	a := herdr.Agent{Name: "probe", Agent: "pi", AgentStatus: status, PaneID: "wP:p1"}
	if withSession {
		a.Session = &herdr.AgentSessionRef{Kind: "path", Value: "/tmp/probe.jsonl"}
	}
	return a
}

func startWatcher(ctx context.Context, f herdrAPI, events chan Event) *Watcher {
	w := &Watcher{
		HC:        f,
		Reconcile: 2 * time.Millisecond,
		Stability: time.Millisecond,
		WaitMax:   100 * time.Millisecond,
		Rearm:     time.Millisecond,
		OnEvent: func(e Event) {
			events <- e
		},
	}
	go w.Run(ctx)
	return w
}

func TestTransitionNotifies(t *testing.T) {
	f := &fakeHerdr{
		agents: []herdr.Agent{probeAgent("idle", true)},
		script: map[string][]waitStep{"wP:p1": {{
			ag: probeAgent("done", true),
			then: func(f *fakeHerdr) {
				f.setAgents(probeAgent("done", true))
			},
		}}},
	}
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWatcher(ctx, f, events)

	select {
	case e := <-events:
		if e.Path != "/tmp/probe.jsonl" || e.Status != "done" || e.Name != "probe" {
			t.Fatalf("event: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("no event")
	}
}

func TestBounceSuppressed(t *testing.T) {
	// The wait reports done, but the state flips back to working inside the
	// bounce window, so the transition never settles.
	f := &fakeHerdr{
		agents: []herdr.Agent{probeAgent("working", true)},
		script: map[string][]waitStep{"wP:p1": {{
			ag: probeAgent("done", true),
			then: func(f *fakeHerdr) {
				f.setAgents(probeAgent("working", true))
			},
		}}},
	}
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWatcher(ctx, f, events)

	select {
	case e := <-events:
		t.Fatalf("bounced transition notified: %+v", e)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestNoSessionNoNotify(t *testing.T) {
	f := &fakeHerdr{
		agents: []herdr.Agent{probeAgent("idle", false)},
		script: map[string][]waitStep{"wP:p1": {{ag: probeAgent("done", false)}}},
	}
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWatcher(ctx, f, events)

	select {
	case e := <-events:
		t.Fatalf("sessionless agent notified: %+v", e)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestTimeoutReArmThenNotify(t *testing.T) {
	f := &fakeHerdr{
		agents: []herdr.Agent{probeAgent("idle", true)},
		script: map[string][]waitStep{"wP:p1": {
			{err: context.DeadlineExceeded}, // herdr timeout, watcher re-arms
			{
				ag: probeAgent("done", true),
				then: func(f *fakeHerdr) {
					f.setAgents(probeAgent("done", true))
				},
			},
		}},
	}
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWatcher(ctx, f, events)

	select {
	case e := <-events:
		if e.Status != "done" {
			t.Fatalf("event: %+v", e)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no event after re-arm")
	}
}

func TestIdleAfterDoneSuppressed(t *testing.T) {
	f := &fakeHerdr{
		agents: []herdr.Agent{probeAgent("working", true)},
		script: map[string][]waitStep{"wP:p1": {
			{
				ag: probeAgent("done", true),
				then: func(f *fakeHerdr) {
					f.setAgents(probeAgent("done", true))
				},
			},
			{
				ag: probeAgent("idle", true),
				then: func(f *fakeHerdr) {
					f.setAgents(probeAgent("idle", true))
				},
			},
		}},
	}
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWatcher(ctx, f, events)

	select {
	case e := <-events:
		if e.Status != "done" {
			t.Fatalf("first event: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("no done event")
	}
	select {
	case e := <-events:
		t.Fatalf("idle after done notified: %+v", e)
	case <-time.After(150 * time.Millisecond):
	}
}

func TestRepeatSameStatusSuppressed(t *testing.T) {
	// herdr re-fires the wait on every agent update while the status holds;
	// the same done must push exactly once.
	step := waitStep{
		ag: probeAgent("done", true),
		then: func(f *fakeHerdr) {
			f.setAgents(probeAgent("done", true))
		},
	}
	f := &fakeHerdr{
		agents: []herdr.Agent{probeAgent("idle", true)},
		script: map[string][]waitStep{"wP:p1": {step}}, // last step repeats
	}
	events := make(chan Event, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWatcher(ctx, f, events)

	select {
	case <-events:
	case <-time.After(time.Second):
		t.Fatal("no done event")
	}
	select {
	case e := <-events:
		t.Fatalf("duplicate event: %+v", e)
	case <-time.After(120 * time.Millisecond):
	}
}

func TestWorkingResetsDedupe(t *testing.T) {
	// A done pushes once; after the agent returns to working and finishes
	// again, the new working->done cycle must push too.
	// The first step carries the hook so the first stillSame sees done; the
	// quiet repeat exists because a hook on the repeating step would re-set
	// done on every fake AgentWait and erase the test's working window
	// before the reconcile tick could ever observe it.
	stepDone := waitStep{
		ag: probeAgent("done", true),
		then: func(f *fakeHerdr) {
			f.setAgents(probeAgent("done", true))
		},
	}
	stepDoneQuiet := waitStep{ag: probeAgent("done", true)}
	f := &fakeHerdr{
		agents: []herdr.Agent{probeAgent("working", true)},
		script: map[string][]waitStep{"wP:p1": {stepDone, stepDoneQuiet}},
	}
	events := make(chan Event, 8)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWatcher(ctx, f, events)

	select {
	case e := <-events:
		if e.Status != "done" {
			t.Fatalf("first event: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("no done event")
	}
	f.setAgents(probeAgent("working", true))
	// The 2ms reconcile tick must observe working inside this window. Suite
	// load can starve the watcher goroutine well past 250ms, so keep margin.
	time.Sleep(600 * time.Millisecond)
	f.setAgents(probeAgent("done", true))
	select {
	case e := <-events:
		if e.Status != "done" {
			t.Fatalf("second event: %+v", e)
		}
	case <-time.After(4 * time.Second):
		t.Fatal("second cycle suppressed: dedupe did not reset on working")
	}
	select {
	case e := <-events:
		t.Fatalf("unexpected third event: %+v", e)
	case <-time.After(500 * time.Millisecond):
	}
}

func TestNewAgentPickedUp(t *testing.T) {
	f := &fakeHerdr{
		agents: []herdr.Agent{probeAgent("idle", true)},
		script: map[string][]waitStep{},
	}
	events := make(chan Event, 4)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	startWatcher(ctx, f, events)

	second := probeAgent("working", true)
	second.PaneID = "wP:p2"
	blocked := second
	blocked.AgentStatus = "blocked"
	f.mu.Lock()
	f.script["wP:p1"] = []waitStep{{ag: probeAgent("done", true)}}
	f.script["wP:p2"] = []waitStep{{
		ag: blocked,
		then: func(f *fakeHerdr) {
			f.setAgents(probeAgent("idle", true), blocked)
		},
	}}
	f.mu.Unlock()
	f.setAgents(probeAgent("idle", true), second)

	select {
	case e := <-events:
		if e.Status != "blocked" {
			t.Fatalf("event: %+v", e)
		}
	case <-time.After(time.Second):
		t.Fatal("second agent never watched")
	}
}
