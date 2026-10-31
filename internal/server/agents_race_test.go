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
	if err := a.runAgent(context.Background(), "p1", "wanted", "hello", nil, func() {}); err != nil {
		t.Fatalf("transient inactive name must retry: %v", err)
	}
	if got := atomic.LoadInt32(&prompts); got != 2 {
		t.Fatalf("prompts = %d, want the refused prompt plus one retry", got)
	}
}
