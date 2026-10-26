package server

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"
)

func newAuthApp(token string) *app {
	return &app{logger: slog.New(slog.NewTextHandler(os.Stderr, nil)), accessToken: token}
}

func probe(a *app, path string, header string, cookie string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(http.MethodGet, path, nil)
	req.Host = "127.0.0.1:8090"
	if header != "" {
		req.Header.Set("Authorization", "Bearer "+header)
	}
	if cookie != "" {
		req.AddCookie(&http.Cookie{Name: "picouch_token", Value: cookie})
	}
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	rec := httptest.NewRecorder()
	a.auth(next).ServeHTTP(rec, req)
	return rec
}

func TestAuthOpenWhenNoToken(t *testing.T) {
	a := newAuthApp("")
	for _, path := range []string{"/api/sessions", "/stream/terminal/k", "/healthz", "/"} {
		if rec := probe(a, path, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("%s: no token configured must stay open, got %d", path, rec.Code)
		}
	}
}

func TestAuthGatesApiAndStreamOnly(t *testing.T) {
	a := newAuthApp("secret")
	// Static surfaces and healthz stay open so the WebView can load the
	// shell before any credential exists.
	for _, path := range []string{"/healthz", "/", "/assets/x.js"} {
		if rec := probe(a, path, "", ""); rec.Code != http.StatusOK {
			t.Fatalf("%s: static must stay open, got %d", path, rec.Code)
		}
	}
	for _, path := range []string{"/api/sessions", "/stream/terminal/k", "/api/stream/sessions"} {
		if rec := probe(a, path, "", ""); rec.Code != http.StatusUnauthorized {
			t.Fatalf("%s: bare request must 401, got %d", path, rec.Code)
		}
	}
}

func TestAuthAcceptsHeaderAndCookie(t *testing.T) {
	a := newAuthApp("secret")
	if rec := probe(a, "/api/sessions", "secret", ""); rec.Code != http.StatusOK {
		t.Fatalf("bearer header must pass, got %d", rec.Code)
	}
	if rec := probe(a, "/api/sessions", "", "secret"); rec.Code != http.StatusOK {
		t.Fatalf("cookie must pass, got %d", rec.Code)
	}
	if rec := probe(a, "/api/sessions", "wrong", "wrong"); rec.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token must 401, got %d", rec.Code)
	}
}
