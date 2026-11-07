package web

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"testing/fstest"
)

func gz(b []byte) []byte {
	var buf bytes.Buffer
	w := gzip.NewWriter(&buf)
	if _, err := w.Write(b); err != nil {
		panic(err)
	}
	if err := w.Close(); err != nil {
		panic(err)
	}
	return buf.Bytes()
}

func fixtureFS() fstest.MapFS {
	rawJS := []byte("console.log('app')")
	rawIndex := []byte("<html><body>pi couch</body></html>")
	return fstest.MapFS{
		"index.html":           &fstest.MapFile{Data: rawIndex},
		"index.html.gz":        &fstest.MapFile{Data: gz(rawIndex)},
		"assets/app-abc.js":    &fstest.MapFile{Data: rawJS},
		"assets/app-abc.js.gz": &fstest.MapFile{Data: gz(rawJS)},
		"assets/pic.png":       &fstest.MapFile{Data: []byte("pngbytes")},
		"LICENSE":              &fstest.MapFile{Data: []byte("MIT")},
		"sw.js":                &fstest.MapFile{Data: []byte("self.addEventListener(0)")},
		"manifest.webmanifest": &fstest.MapFile{Data: []byte(`{"name":"pi couch"}`)},
	}
}

func serve(t *testing.T, path, acceptEncoding string) *httptest.ResponseRecorder {
	t.Helper()
	h := appHandler(fixtureFS())
	req := httptest.NewRequest(http.MethodGet, path, nil)
	if acceptEncoding != "" {
		req.Header.Set("Accept-Encoding", acceptEncoding)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestAssetsServePrecompressedGzip(t *testing.T) {
	rec := serve(t, "/assets/app-abc.js", "gzip, deflate, br")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if got := rec.Header().Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q", got)
	}
	if got := rec.Header().Get("Content-Type"); got != "text/javascript; charset=utf-8" {
		t.Fatalf("Content-Type = %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if string(decoded) != "console.log('app')" {
		t.Fatalf("decoded body = %q", decoded)
	}
}

func TestAssetsServeRawWithoutAcceptEncoding(t *testing.T) {
	for _, ae := range []string{"", "identity"} {
		rec := serve(t, "/assets/app-abc.js", ae)
		if rec.Code != http.StatusOK {
			t.Fatalf("accept %q: status %d", ae, rec.Code)
		}
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Fatalf("accept %q: Content-Encoding = %q", ae, got)
		}
		if got := rec.Body.String(); got != "console.log('app')" {
			t.Fatalf("accept %q: body = %q", ae, got)
		}
	}
}

func TestAssetsWithoutSidecarStayRaw(t *testing.T) {
	rec := serve(t, "/assets/pic.png", "gzip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if got := rec.Body.String(); got != "pngbytes" {
		t.Fatalf("body = %q", got)
	}
}

// Compressible extension, but the build has not emitted a sidecar for
// it (the manifest case on a fresh clone): raw bytes must flow.
func TestInSetExtensionWithoutSidecarStaysRaw(t *testing.T) {
	rec := serve(t, "/manifest.webmanifest", "gzip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if got := rec.Body.String(); got != `{"name":"pi couch"}` {
		t.Fatalf("body = %q", got)
	}
}

func TestAcceptsGzipHeaderParsing(t *testing.T) {
	cases := []struct {
		header string
		want   bool
	}{
		{"", false},
		{"gzip", true},
		{" gzip ", true},
		{"GZIP", true},
		{"deflate", false},
		{"br, gzip", true},
		{"gzip;q=0", false},
		{"gzip; q=0", false},
		{"gzip;q=0.5", true},
		{"gzip;q=0.001", true},
		{"br,gzip;q=0", false},
		{"gzip;q=abc", true},
		{"gzip,gzip;q=0", false},
		{"gzip;q=0,gzip", true},
	}
	for _, tc := range cases {
		if got := gzipWanted(tc.header); got != tc.want {
			t.Errorf("gzipWanted(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

// A dotless embedded file must not panic the ext lookup (path.Ext
// returns "" for it).
func TestDotlessFileServesRaw(t *testing.T) {
	rec := serve(t, "/LICENSE", "gzip")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if got := rec.Body.String(); got != "MIT" {
		t.Fatalf("body = %q", got)
	}
}

func TestIndexFallbackServesGzip(t *testing.T) {
	rec := serve(t, "/", "gzip")
	if got := rec.Header().Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	zr, err := gzip.NewReader(rec.Body)
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(decoded, []byte("pi couch")) {
		t.Fatalf("decoded = %q", decoded)
	}
}

func TestIndexFallbackServesRaw(t *testing.T) {
	rec := serve(t, "/", "")
	if got := rec.Header().Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("pi couch")) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}

func TestSpaRouteFallsBackToIndex(t *testing.T) {
	rec := serve(t, "/settings", "")
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	if !bytes.Contains(rec.Body.Bytes(), []byte("pi couch")) {
		t.Fatalf("body = %q", rec.Body.String())
	}
}
