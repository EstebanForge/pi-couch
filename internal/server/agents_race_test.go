package server

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

func TestWaitAgentActiveRequiresTheStartedName(t *testing.T) {
	sess := ""
	status := "working"
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sess, status: &status, agentName: "other"})
	client := herdr.NewAt(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Millisecond)
	defer cancel()
	if err := waitAgentActive(ctx, client, "p1", "wanted"); err == nil {
		t.Fatal("another agent's active status must not ready the new name")
	}
}

func TestWaitAgentActiveRequiresBoundSession(t *testing.T) {
	sess := ""
	status := "working"
	var unbound atomic.Bool
	unbound.Store(true)
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sess, status: &status, agentName: "wanted", sessionUnbound: &unbound})
	client := herdr.NewAt(sock)
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	if err := waitAgentActive(ctx, client, "p1", "wanted"); err == nil {
		t.Fatal("an active status without a bound session must not ready the prompt gate")
	}
}

func TestWaitAgentActiveWaitsForTheSessionBind(t *testing.T) {
	sess := ""
	status := "working"
	var unbound atomic.Bool
	unbound.Store(true)
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sess, status: &status, agentName: "wanted", sessionUnbound: &unbound})
	client := herdr.NewAt(sock)
	go func() {
		time.Sleep(400 * time.Millisecond)
		unbound.Store(false)
	}()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if err := waitAgentActive(ctx, client, "p1", "wanted"); err != nil {
		t.Fatalf("a session bind at 400ms must ready the agent: %v", err)
	}
}

func TestRunAgentPromptSurvivesABindingGap(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(sess, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := "working"
	ready := "0.0%/1.0M"
	var unbound atomic.Bool
	unbound.Store(true)
	var sends int32
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sess, status: &status, agentName: "wanted", paneReady: &ready,
		sessionUnbound: &unbound, sends: &sends,
	})
	go func() {
		time.Sleep(2 * time.Second)
		unbound.Store(false)
	}()
	a := &app{herdr: herdr.NewAt(sock), logger: slog.New(slog.NewTextHandler(testDiscard{}, nil))}
	if err := a.runAgent(context.Background(), "p1", "wanted", "hello", nil, func() {}, func() {}); err != nil {
		t.Fatalf("a 2s binding gap must ride out via readiness wait and prompt retries: %v", err)
	}
}

// herdr's prompt gate can stay shut seconds after agent.list already
// reports the agent named, bound, and non-unknown (observed >5.5s on a
// real create); the retry must be deadline-based, not attempt-capped.
func TestRunAgentPromptOutlastsALongGateGap(t *testing.T) {
	every, deadline := promptRetryEvery, promptRetryDeadline
	promptRetryEvery, promptRetryDeadline = 50*time.Millisecond, 5*time.Second
	defer func() { promptRetryEvery, promptRetryDeadline = every, deadline }()
	dir := t.TempDir()
	sess := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(sess, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := "working"
	ready := "0.0%/1.0M"
	var sends int32
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sess, status: &status, agentName: "wanted", paneReady: &ready,
		sends: &sends, promptRefuseUntil: time.Now().Add(1500 * time.Millisecond),
	})
	a := &app{herdr: herdr.NewAt(sock), logger: slog.New(slog.NewTextHandler(testDiscard{}, nil))}
	if err := a.runAgent(context.Background(), "p1", "wanted", "hello", nil, func() {}, func() {}); err != nil {
		t.Fatalf("a 1.5s prompt-gate gap must ride out via deadline retries: %v", err)
	}
}

// A delivered prompt is the point of no return: when the session
// never verifies (pi wrote no jsonl yet), the create must fail
// honestly but leave the agent running. Teardown here killed agents
// whose prompt herdr had already accepted (2026-11-02 quick-6bb8).
func TestCreateKeepsTheAgentWhenNoSessionVerifies(t *testing.T) {
	every, deadline, verify := promptRetryEvery, promptRetryDeadline, sessionVerifyWait
	promptRetryEvery, promptRetryDeadline = 100*time.Millisecond, 5*time.Second
	sessionVerifyWait = 200 * time.Millisecond
	defer func() { promptRetryEvery, promptRetryDeadline, sessionVerifyWait = every, deadline, verify }()
	dir := t.TempDir()
	phantom := filepath.Join(dir, "never.jsonl")
	status := "working"
	ready := "0.0%/1.0M"
	var sends, closes int32
	// herdr reports the bind while the agent readies, then it stops
	// reporting a session at all (pi wedged pre-write tonight) — the
	// key fetch then polls a dead context and the old code pane.closed
	// the agent herdr had just accepted a prompt for. The unbind is
	// event-driven: the fake drops the bind at the instant it accepts
	// the prompt, so no wall-clock window can race the delivery.
	var unbound atomic.Bool
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &phantom, status: &status, paneReady: &ready,
		sends: &sends, promptRefuseUntil: time.Now().Add(300 * time.Millisecond),
		paneCloses: &closes, sessionUnbound: &unbound,
		onPrompt: func() { unbound.Store(true) },
	})
	a := &app{herdr: herdr.NewAt(sock), logger: slog.New(slog.NewTextHandler(testDiscard{}, nil))}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, _, err := a.createInWorkspace(ctx, "w1", "hello", nil)
	if err == nil {
		t.Fatal("an unverified session must fail the create honestly")
	}
	if got := atomic.LoadInt32(&closes); got != 0 {
		t.Fatalf("pane.close ran %d times after the prompt was delivered; the agent must survive", got)
	}
}

func TestRunAgentRetriesTransientInactiveName(t *testing.T) {
	dir := t.TempDir()
	sess := filepath.Join(dir, "s.jsonl")
	if err := os.WriteFile(sess, []byte("{}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	status := "working"
	ready := "0.0%/1.0M"
	var prompts int32
	var sends int32
	sock := herdrFake(t, fakeHerdrOpts{
		sessPath: &sess, status: &status, agentName: "wanted", paneReady: &ready,
		failFirstPromptNotActive: true, prompts: &prompts, sends: &sends,
	})
	a := &app{herdr: herdr.NewAt(sock), logger: slog.New(slog.NewTextHandler(testDiscard{}, nil))}
	if err := a.runAgent(context.Background(), "p1", "wanted", "hello", nil, func() {}, func() {}); err != nil {
		t.Fatalf("transient inactive name must retry: %v", err)
	}
	if got := atomic.LoadInt32(&prompts); got != 2 {
		t.Fatalf("prompts = %d, want the refused prompt plus one retry", got)
	}
}
