package server

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

// apiSessionsFixture builds an index over one real-shaped session file.
func apiSessionsFixture(t *testing.T) *pi.Index {
	t.Helper()
	dir := t.TempDir()
	sub := filepath.Join(dir, "--tmp-couchpi-web--")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	lines := []string{
		`{"type":"session","version":3,"id":"11111111-2222-3333-4444-555555555555","timestamp":"2026-10-10T12:00:00.000Z","cwd":"/tmp/proj"}`,
		`{"type":"message","id":"e1","parentId":null,"timestamp":"2026-10-10T12:00:01.000Z","message":{"role":"user","content":"hello from the fixture","timestamp":1760000001000}}`,
	}
	if err := os.WriteFile(filepath.Join(sub, "2026-10-10T12-00-00-000Z_11111111-2222-3333-4444-555555555555.jsonl"), []byte(lines[0]+"\n"+lines[1]+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return pi.NewIndex(dir)
}

func TestAPISessionsShape(t *testing.T) {
	a := &app{
		logger: slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:    apiSessionsFixture(t),
		herdr:  herdr.New(),
	}
	srv := httptest.NewServer(http.HandlerFunc(a.apiSessions))
	defer srv.Close()

	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /api/sessions = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("content type %q, want application/json", ct)
	}
	var snap struct {
		Sessions []struct {
			Key       string `json:"key"`
			FirstUser string `json:"first_user"`
			Mtime     string `json:"mtime"`
		} `json:"sessions"`
		Badges map[string]string `json:"badges"`
	}
	if err := json.NewDecoder(res.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if len(snap.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(snap.Sessions))
	}
	if snap.Sessions[0].FirstUser != "hello from the fixture" {
		t.Fatalf("first_user %q", snap.Sessions[0].FirstUser)
	}
	if snap.Sessions[0].Mtime == "" {
		t.Fatal("mtime must marshal as an RFC3339 string")
	}
	if snap.Badges == nil {
		t.Fatal("badges must marshal, never null")
	}
}

func TestAPISessionsEmptyIndexNoError(t *testing.T) {
	a := &app{
		logger: slog.New(slog.NewTextHandler(testDiscard{}, nil)),
		idx:    pi.NewIndex(t.TempDir()),
		herdr:  herdr.New(),
	}
	srv := httptest.NewServer(http.HandlerFunc(a.apiSessions))
	defer srv.Close()
	res, err := http.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("empty index = %d, want 200", res.StatusCode)
	}
	var snap struct {
		Sessions []struct{}        `json:"sessions"`
		Badges   map[string]string `json:"badges"`
	}
	if err := json.NewDecoder(res.Body).Decode(&snap); err != nil {
		t.Fatal(err)
	}
	if snap.Sessions == nil || snap.Badges == nil {
		t.Fatal("empty snapshot must marshal as [] and {}, never null")
	}
}
