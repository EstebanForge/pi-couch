package web

import (
	"io"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// hasIndex reports whether a built SPA is embedded. A fresh clone carries
// only .gitkeep until scripts/build.sh runs, so tests skip then.
func hasIndex(t *testing.T) bool {
	t.Helper()
	_, err := fs.Stat(appFS, "app/index.html")
	return err == nil
}

func TestAppServesIndex(t *testing.T) {
	if !hasIndex(t) {
		t.Skip("no built spa; run scripts/build.sh")
	}
	srv := httptest.NewServer(App())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET / = %d, want 200", res.StatusCode)
	}
	if ct := res.Header.Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("content type %q, want text/html", ct)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("index cache-control %q, want no-cache", cc)
	}
	body, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(body), `id="root"`) {
		t.Fatal("served index lacks the react root div")
	}
}

func TestAppIndexCSP(t *testing.T) {
	if !hasIndex(t) {
		t.Skip("no built spa; run scripts/build.sh")
	}
	srv := httptest.NewServer(App())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/sessions/deep/link")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	csp := res.Header.Get("Content-Security-Policy")
	for _, want := range []string{"default-src 'none'", "script-src 'self'", "connect-src 'self'", "style-src 'self' 'unsafe-inline'"} {
		if !strings.Contains(csp, want) {
			t.Errorf("CSP missing %q in %q", want, csp)
		}
	}
}

func TestAppSpaRouteFallsBackToIndex(t *testing.T) {
	if !hasIndex(t) {
		t.Skip("no built spa; run scripts/build.sh")
	}
	srv := httptest.NewServer(App())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/sessions/somekey")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /sessions/somekey = %d, want 200 from the spa fallback", res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); cc != "no-cache" {
		t.Fatalf("spa fallback cache-control %q, want no-cache", cc)
	}
}

func TestAppAssetImmutable(t *testing.T) {
	if !hasIndex(t) {
		t.Skip("no built spa; run scripts/build.sh")
	}
	matches, err := fs.Glob(appFS, "app/assets/*.js")
	if err != nil || len(matches) == 0 {
		t.Skip("no built assets")
	}
	name := strings.TrimPrefix(matches[0], "app/")
	srv := httptest.NewServer(App())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/" + name)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatalf("GET /%s = %d, want 200", name, res.StatusCode)
	}
	if cc := res.Header.Get("Cache-Control"); !strings.Contains(cc, "immutable") {
		t.Fatalf("asset cache-control %q, want immutable", cc)
	}
}

func TestAppMissingAssetNotFound(t *testing.T) {
	srv := httptest.NewServer(App())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/assets/never-built.js")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("missing asset = %d, want 404", res.StatusCode)
	}
}

func TestAppDirPathNotFound(t *testing.T) {
	if !hasIndex(t) {
		t.Skip("no built spa; run scripts/build.sh")
	}
	srv := httptest.NewServer(App())
	defer srv.Close()
	res, err := http.Get(srv.URL + "/assets/")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	if res.StatusCode != 404 {
		t.Fatalf("asset dir = %d, want 404", res.StatusCode)
	}
}
