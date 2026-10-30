package server

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

// fakeStatus stands in for any StatusReader in wiring tests.
type fakeStatus struct {
	badges map[string]string
}

func (f *fakeStatus) Badges(_ context.Context) map[string]string { return f.badges }

func (f *fakeStatus) Driven(_ context.Context) map[string]herdr.DrivenInfo {
	return map[string]herdr.DrivenInfo{}
}

func (f *fakeStatus) DriveStatus(_ context.Context) (map[string]string, map[string]herdr.DrivenInfo) {
	return f.badges, map[string]herdr.DrivenInfo{}
}

func (f *fakeStatus) TargetForPath(_ context.Context, path string) (string, error) {
	return "", herdr.ErrNotDriven
}

// TestHCModeRefusesHerdrCommands pins the wave-3 state: with the hc
// driver wired (status set, herdr nil) the command surfaces refuse
// loudly instead of dialing the wrong socket.
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
	req := httptest.NewRequest(http.MethodPost, "/api/agents", bytes.NewBufferString(`{"cwd":"/home/me/x","prompt":"hi"}`))
	rec := httptest.NewRecorder()
	a.apiAgents(rec, req)
	if rec.Code != http.StatusNotImplemented {
		t.Fatalf("code %d, want 501", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("unsupported-driver")) {
		t.Fatalf("body %q, want the unsupported-driver error", rec.Body.String())
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
