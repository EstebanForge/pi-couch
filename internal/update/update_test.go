package update

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
)

const fakeBinaryNew = "#!/bin/sh\necho 9.9.9\n"

func fakeAPIServer(t *testing.T, tag, token string, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		if token != "" && r.Header.Get("Authorization") != "Bearer "+token {
			t.Errorf("missing token header")
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
}

func fakeDownloadServer(t *testing.T, body string, hits *int) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*hits++
		_, _ = w.Write([]byte(body))
	}))
}

// stage wires an Updater against fake servers with a temp "binary".
// It returns the current-binary path, a restart recorder, and the
// download hit count.
func stage(t *testing.T, tag, token, serveBody string) (string, *[]string, *int) {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "couch-pi")
	if err := os.WriteFile(bin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var apiHits, dlHits int
	api := fakeAPIServer(t, tag, token, &apiHits)
	t.Cleanup(api.Close)
	dl := fakeDownloadServer(t, serveBody, &dlHits)
	t.Cleanup(dl.Close)

	prevAPI, prevDL, prevBin, prevRestart := latestAPI, latestDownload, binaryPath, restart
	latestAPI, latestDownload, binaryPath = api.URL, dl.URL+"/", func() string { return bin }
	restarted := &[]string{}
	var mu sync.Mutex
	restart = func(path string) error {
		mu.Lock()
		*restarted = append(*restarted, path)
		mu.Unlock()
		return nil
	}
	t.Cleanup(func() {
		latestAPI, latestDownload, binaryPath, restart = prevAPI, prevDL, prevBin, prevRestart
	})

	u := New(testLogger(), "0.1.45", token)
	u.checkAndMaybeRestart()
	return bin, restarted, &dlHits
}

func TestCheckSwapsAndRestarts(t *testing.T) {
	bin, restarted, _ := stage(t, "9.9.9", "tok", fakeBinaryNew)

	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != fakeBinaryNew {
		t.Fatalf("binary not swapped: %q", string(got))
	}
	bak, err := os.ReadFile(bin + ".bak")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if string(bak) != "old-binary" {
		t.Fatalf("backup = %q, want old content", string(bak))
	}
	if len(*restarted) != 1 || (*restarted)[0] != bin {
		t.Fatalf("restart = %v, want [%s]", *restarted, bin)
	}
}

func TestCheckSkipsOlderTag(t *testing.T) {
	_, restarted, dlHits := stage(t, "0.0.1", "", fakeBinaryNew)
	if len(*restarted) != 0 {
		t.Fatalf("restarted on older tag")
	}
	if *dlHits != 0 {
		t.Fatalf("downloaded on older tag (%d hits)", *dlHits)
	}
}

func TestCheckAbortsWhenProbeFails(t *testing.T) {
	bin, restarted, _ := stage(t, "9.9.9", "", "#!/bin/sh\necho nope\n")

	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-binary" {
		t.Fatalf("binary swapped despite bad probe: %q", string(got))
	}
	if len(*restarted) != 0 {
		t.Fatalf("restarted despite bad probe")
	}
}

func TestCmpVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"0.1.46", "0.1.45", 1},
		{"0.2.0", "0.1.99", 1},
		{"1.0.0", "1.0.0", 0},
		{"0.1.9", "0.1.10", -1},
		{"garbage", "0.1.45", -1},
	}
	for _, c := range cases {
		if got := cmpVersion(c.a, c.b); got != c.want {
			t.Errorf("cmpVersion(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
