package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func guardApp(origin string) http.Handler {
	a := &app{logger: slog.New(slog.NewTextHandler(testDiscard{}, nil)), allowedOrigin: origin}
	return a.guard(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
}

type testDiscard struct{}

func (testDiscard) Write(p []byte) (int, error) { return len(p), nil }

func post(target, host string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodPost, target, nil)
	if host != "" {
		req.Host = host
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	guardApp("https://box.example.ts.net").ServeHTTP(rec, req)
	return rec
}

func TestGuardAllowsPlainClient(t *testing.T) {
	// curl, the native door, and the test receiver send no browser headers.
	if rec := post("/sessions/k/prompt", "127.0.0.1:8090", nil); rec.Code != http.StatusOK {
		t.Fatalf("plain POST: %d", rec.Code)
	}
}

func TestGuardAllowsSameOrigin(t *testing.T) {
	h := map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://box.example.ts.net"}
	if rec := post("/x", "box.example.ts.net", h); rec.Code != http.StatusOK {
		t.Fatalf("same-origin POST: %d", rec.Code)
	}
	// tailscale serve may rewrite Host to the loopback target.
	h2 := map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://box.example.ts.net"}
	if rec := post("/x", "127.0.0.1:8090", h2); rec.Code != http.StatusOK {
		t.Fatalf("same-origin via rewritten host: %d", rec.Code)
	}
}

func TestGuardBlocksCrossSite(t *testing.T) {
	h := map[string]string{"Sec-Fetch-Site": "cross-site", "Origin": "https://evil.example"}
	if rec := post("/x", "box.example.ts.net", h); rec.Code != http.StatusForbidden {
		t.Fatalf("cross-site POST: %d", rec.Code)
	}
}

func TestGuardBlocksForeignOrigin(t *testing.T) {
	h := map[string]string{"Origin": "https://evil.example"}
	if rec := post("/x", "box.example.ts.net", h); rec.Code != http.StatusForbidden {
		t.Fatalf("foreign origin POST: %d", rec.Code)
	}
}

func TestGuardBlocksReboundHost(t *testing.T) {
	// DNS rebinding: same-origin by every browser signal, wrong host.
	h := map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "http://evil.example:8090"}
	if rec := post("/x", "evil.example:8090", h); rec.Code != http.StatusForbidden {
		t.Fatalf("rebound host POST: %d", rec.Code)
	}
}

func TestGuardAllowsOriginOnAlternatePort(t *testing.T) {
	// tailscale serve moved to :8443 (Wicket owns :443 machine-wide): the
	// Origin header carries the port, the -origin flag may not. Hostname
	// equality decides; ports do not mint new trusted servers.
	h := map[string]string{"Sec-Fetch-Site": "same-origin", "Origin": "https://box.example.ts.net:8443"}
	if rec := post("/x", "box.example.ts.net:8443", h); rec.Code != http.StatusOK {
		t.Fatalf("alt-port origin POST: %d", rec.Code)
	}
	// Host rewritten to the loopback target must still pass via the flag.
	if rec := post("/x", "127.0.0.1:8090", h); rec.Code != http.StatusOK {
		t.Fatalf("alt-port origin via rewritten host: %d", rec.Code)
	}
}

func TestGuardAllowsGetUnconditionally(t *testing.T) {
	req := httptest.NewRequest(http.MethodGet, "/x", nil)
	req.Host = "evil.example"
	req.Header.Set("Sec-Fetch-Site", "cross-site")
	rec := httptest.NewRecorder()
	guardApp("").ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET blocked: %d", rec.Code)
	}
}
