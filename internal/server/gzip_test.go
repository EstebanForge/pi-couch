package server

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func bytesReader(b []byte) *bytes.Reader { return bytes.NewReader(b) }

func streamContains(b []byte, prefix string) bool {
	return bytes.Contains(b, []byte(prefix))
}

func TestAPIGzipModeRouting(t *testing.T) {
	cases := []struct {
		method, path string
		want         gzipMode
	}{
		{"GET", "/api/version", gzipBuffered},
		{"GET", "/api/sessions", gzipBuffered},
		{"GET", "/api/sessions/k/changes", gzipBuffered},
		{"GET", "/api/sessions/k/items/3", gzipBuffered},
		{"HEAD", "/api/models", gzipBuffered},
		{"GET", "/api/stream/transcript/s1", gzipFlush},
		{"GET", "/api/stream/sessions", gzipNone},
		{"GET", "/api/sessions/k/items/3/image", gzipNone},
		{"POST", "/api/sessions/k/prompt", gzipNone},
		{"GET", "/stream/terminal/k", gzipNone},
		{"GET", "/healthz", gzipNone},
		{"GET", "/", gzipNone},
	}
	for _, tc := range cases {
		if got := apiGzipMode(tc.method, tc.path); got != tc.want {
			t.Errorf("%s %s: mode = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}

func gzipJSONSize(t *testing.T, accept string) (*http.Response, []byte) {
	t.Helper()
	_, _, h := streamTestApp(t)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodGet, srv.URL+"/api/version", nil)
	if err != nil {
		t.Fatal(err)
	}
	if accept != "" {
		req.Header.Set("Accept-Encoding", accept)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	return resp, raw
}

func TestAPIJSONCompressesWhenAccepted(t *testing.T) {
	resp, raw := gzipJSONSize(t, "gzip")
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	if got := resp.Header.Get("Vary"); got != "Accept-Encoding" {
		t.Fatalf("Vary = %q", got)
	}
	zr, err := gzip.NewReader(bytesReader(raw))
	if err != nil {
		t.Fatalf("body is not gzip: %v", err)
	}
	decoded, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var version struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(decoded, &version); err != nil {
		t.Fatalf("decoded %q: %v", decoded, err)
	}
	if version.Version == "" {
		t.Fatalf("decoded %q carries no version", decoded)
	}
}

func TestAPIJSONStaysRawWithoutAcceptEncoding(t *testing.T) {
	resp, raw := gzipJSONSize(t, "")
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	var version struct {
		Version string `json:"version"`
	}
	if err := json.Unmarshal(raw, &version); err != nil {
		t.Fatalf("body %q: %v", raw, err)
	}
}

// Terminal and sessions SSE keep tiny frequent frames where gzip flush
// overhead would cost more than it saves, so they must stay plaintext.
func TestStreamsOtherThanTranscriptStayRaw(t *testing.T) {
	_, _, h := streamTestApp(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream/sessions", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
	buf := make([]byte, 256)
	n, _ := io.ReadFull(resp.Body, buf)
	if n == 0 {
		t.Fatal("no bytes streamed")
	}
	if !streamContains(buf[:n], "event:") {
		t.Fatalf("stream prefix %q is not plaintext SSE", buf[:n])
	}
}

// The transcript's first frame is a full session snapshot (tens of KB
// on long sessions); compressed, it rides at a fraction of the size.
func TestTranscriptSSECompresses(t *testing.T) {
	_, _, h := streamTestApp(t)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, srv.URL+"/api/stream/transcript/s1", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Encoding"); got != "gzip" {
		t.Fatalf("Content-Encoding = %q", got)
	}
	raw, _ := io.ReadAll(resp.Body)
	if len(raw) < 2 || raw[0] != 0x1f || raw[1] != 0x8b {
		t.Fatalf("body does not start with a gzip header: % x", raw[:min(4, len(raw))])
	}
	zr, err := gzip.NewReader(bytesReader(raw))
	if err != nil {
		t.Fatalf("gzip header rejected: %v", err)
	}
	decoded, _ := io.ReadAll(zr)
	if !streamContains(decoded, "event:") {
		t.Fatalf("decoded stream %q carries no SSE frames", decoded[:min(80, len(decoded))])
	}
}

func TestGzipWrappersLeaveBodylessCodesAlone(t *testing.T) {
	for _, code := range []int{http.StatusNoContent, http.StatusNotModified} {
		rec := httptest.NewRecorder()
		bw := newBufferedGzipWriter(rec)
		bw.WriteHeader(code)
		bw.finish()
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("buffered %d: Content-Encoding = %q", code, got)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("buffered %d: body = %q", code, rec.Body.String())
		}
		fw := newFlushGzipWriter(rec)
		fw.WriteHeader(code)
		fw.finish()
		if got := rec.Header().Get("Content-Encoding"); got != "" {
			t.Errorf("flush %d: Content-Encoding = %q", code, got)
		}
		if rec.Body.Len() != 0 {
			t.Errorf("flush %d: body = %q", code, rec.Body.String())
		}
	}
}

// POST responses must stay uncompressed: bridge and command endpoints
// answer in a few bytes.
func TestPOSTResponsesStayRaw(t *testing.T) {
	_, _, h := streamTestApp(t)
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	req, err := http.NewRequest(http.MethodPost, srv.URL+"/api/sessions/s1/ask/answer", strings.NewReader("{}"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Accept-Encoding", "gzip")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if got := resp.Header.Get("Content-Encoding"); got != "" {
		t.Fatalf("Content-Encoding = %q, want none", got)
	}
}
