package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func healthServer(t *testing.T, status string, code int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/agentmemory/livez" {
			w.WriteHeader(http.StatusOK)
			return
		}
		w.WriteHeader(code)
		w.Write([]byte(`{"status":` + `"` + status + `"}`))
	}))
}

func TestClassifyMemory(t *testing.T) {
	for _, tc := range []struct {
		body string
		want string
	}{
		{`{"status":"healthy"}`, "healthy"},
		{`{"health":{"status":"degraded"}}`, "degraded"},
		{`{"status":"critical"}`, "degraded"},
		{`{"status":"unhealthy"}`, "unhealthy"},
		{`{"status":"down"}`, "down"},
		{`{"dialect":"future"}`, "unknown"},
		{`junk`, "unknown"},
	} {
		if got := classifyMemory([]byte(tc.body)); got != tc.want {
			t.Errorf("classifyMemory(%s) = %q, want %q", tc.body, got, tc.want)
		}
	}
}

func TestMemoryProbeSendsBearerWhenConfigured(t *testing.T) {
	var got string
	hs := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get("Authorization")
		w.Write([]byte(`{"status":"healthy"}`))
	}))
	defer hs.Close()
	m := newMemoryProbeAt(hs.URL)
	m.secret = "tok-123"
	if got2 := m.fetch(); got2 != "healthy" || got != "Bearer tok-123" {
		t.Fatalf("fetch = %q, auth = %q", got2, got)
	}
}

func TestMemoryProbeHealthAndFallback(t *testing.T) {
	hs := healthServer(t, "healthy", http.StatusOK)
	defer hs.Close()
	m := newMemoryProbeAt(hs.URL)
	if got := m.fetch(); got != "healthy" {
		t.Fatalf("fetch = %q, want healthy", got)
	}
}

// A Bearer-gated or 503ing health path falls back to liveness: up but
// unreadable reads unknown, nothing answers reads down.
func TestMemoryProbeFallsBackToLivez(t *testing.T) {
	hs := healthServer(t, "healthy", http.StatusUnauthorized)
	defer hs.Close()
	m := newMemoryProbeAt(hs.URL)
	if got := m.fetch(); got != "unknown" {
		t.Fatalf("gated health fetch = %q, want unknown", got)
	}
	hs.Close()
	if got := m.fetch(); got != "down" {
		t.Fatalf("dead server fetch = %q, want down", got)
	}
}

func TestTranscriptResetCarriesMemory(t *testing.T) {
	hs := healthServer(t, "degraded", http.StatusOK)
	defer hs.Close()
	a, _, h := streamTestApp(t)
	a.memory = newMemoryProbeAt(hs.URL)
	// Warm the probe so the reset frame carries a classified state.
	if got := a.memory.fetch(); got != "degraded" {
		t.Fatalf("warm = %q", got)
	}
	a.memory.seen()
	srv := httptest.NewServer(h)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/stream/transcript/s1")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := readFrames(t, resp.Body)
	f := waitFrame(t, frames, 5*time.Second, "reset")
	var rs transcriptReset
	if err := json.Unmarshal([]byte(f.data), &rs); err != nil {
		t.Fatal(err)
	}
	if rs.Memory != "degraded" {
		t.Fatalf("reset memory = %q, want degraded", rs.Memory)
	}
}
