package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

// fakeStatus stands in for any StatusReader in wiring tests; the
// command log doubles as a CommandDriver.
type fakeStatus struct {
	badges  map[string]string
	targets map[string]string

	mu   sync.Mutex
	cmds []string
}

func (f *fakeStatus) log(cmd string) {
	f.mu.Lock()
	f.cmds = append(f.cmds, cmd)
	f.mu.Unlock()
}

func (f *fakeStatus) Prompt(_ context.Context, target, text string) error {
	f.log("prompt:" + target + ":" + text)
	return nil
}

func (f *fakeStatus) Stop(_ context.Context, target string) error {
	f.log("stop:" + target)
	return nil
}

func (f *fakeStatus) Close(_ context.Context, target string) error {
	f.log("close:" + target)
	return nil
}

func (f *fakeStatus) Badges(_ context.Context) map[string]string { return f.badges }

func (f *fakeStatus) Driven(_ context.Context) map[string]herdr.DrivenInfo {
	return map[string]herdr.DrivenInfo{}
}

func (f *fakeStatus) DriveStatus(_ context.Context) (map[string]string, map[string]herdr.DrivenInfo) {
	return f.badges, map[string]herdr.DrivenInfo{}
}

func (f *fakeStatus) TargetForPath(_ context.Context, path string) (string, error) {
	addr, ok := f.targets[path]
	if !ok {
		return "", herdr.ErrNotDriven
	}
	return addr, nil
}

// TestHCModeRefusesHerdrCommands pins the herdr-only spawn surface:
// with the hc driver wired (status set, herdr nil) workspace listing
// and agent creation refuse loudly instead of dialing the wrong
// socket. Commands and terminal streams run on both drivers since
// waves 4 and 8.
func TestHCModeRefusesHerdrCommands(t *testing.T) {
	a := &app{
		logger:      slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:         pi.NewIndex(filepath.Join(t.TempDir(), "sess")),
		home:        testHome(),
		answered:    newDialogMemos(),
		models:      newModelsCache(filepath.Join(t.TempDir(), "models.json")),
		memory:      newMemoryProbe("", ""),
		status:      &fakeStatus{badges: map[string]string{}},
		dialogLatch: map[string]*dialogWire{},
	}
	req := httptest.NewRequest(http.MethodGet, "/api/workspaces", nil)
	rec := httptest.NewRecorder()
	a.apiWorkspaces(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("code %d, want 501", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("unsupported-driver")) {
		t.Fatalf("body %q, want the unsupported-driver error", rec.Body.String())
	}
}

// TestHCCommandSurfacePrompts pins the wave-4 seam: with only the hc
// driver wired, promptAgent dispatches through the CommandDriver
// interface instead of the herdr client.
func TestHCCommandSurfacePrompts(t *testing.T) {
	root := t.TempDir()
	sub := filepath.Join(root, "sess")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	writeLines(t, filepath.Join(sub, "s1.jsonl"), sessHeader, userLine("a", "", "first"))
	fake := &fakeStatus{badges: map[string]string{}, targets: map[string]string{}}
	a := &app{
		logger:      slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:         pi.NewIndex(root),
		home:        testHome(),
		answered:    newDialogMemos(),
		models:      newModelsCache(filepath.Join(t.TempDir(), "models.json")),
		memory:      newMemoryProbe("", ""),
		status:      fake,
		dialogLatch: map[string]*dialogWire{},
	}
	meta, ok := a.idx.Meta("s1")
	if !ok {
		t.Fatal("session s1 not indexed")
	}
	fake.mu.Lock()
	fake.targets[meta.Path] = "t1"
	fake.mu.Unlock()
	if err := a.promptAgent("s1", "hi"); err != nil {
		t.Fatalf("prompt under hc: %v", err)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if len(fake.cmds) != 1 || !strings.HasPrefix(fake.cmds[0], "prompt:") || !strings.HasSuffix(fake.cmds[0], ":hi") {
		t.Fatalf("commands = %v, want one prompt carrying hi", fake.cmds)
	}
}

// TestStatusFallsBackToHerdr pins the herdr default: a literal with
// only the herdr client wired serves status from it.
func TestStatusFallsBackToHerdr(t *testing.T) {
	sess := filepath.Join(t.TempDir(), "s.jsonl")
	status := "working"
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sess, status: &status})
	a := &app{herdr: herdr.NewAt(sock)}
	if a.statusFor() != a.herdr {
		t.Fatal("statusFor did not fall back to the herdr client")
	}
	badges := a.statusFor().Badges(context.Background())
	if badges[sess] != "working" {
		t.Fatalf("badge = %v, want working", badges)
	}
}
