package update

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

const fakeBinaryNew = "#!/bin/sh\necho 9.9.9\n"

func fakeAPIServer(t *testing.T, tag string, hits *atomic.Int32, sawAuth *atomic.Bool) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if r.Header.Get("Authorization") != "" {
			sawAuth.Store(true)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"` + tag + `"}`))
	}))
}

func fakeDownloadServer(t *testing.T, body string, hits *atomic.Int32, lastPath *atomic.Value) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		lastPath.Store(r.URL.Path)
		_, _ = w.Write([]byte(body))
	}))
}

type stageOut struct {
	bin       string
	restarted *[]string
	dlHits    *atomic.Int32
	dlPath    *atomic.Value
}

// stage wires an Updater against fake servers with a temp "binary".
// The fake asset is a script that prints the tag the test asks for,
// which is what the real -version probe requires.
func stage(t *testing.T, tag, token, serveBody string) stageOut {
	t.Helper()
	dir := t.TempDir()
	bin := filepath.Join(dir, "couch-pi")
	if err := os.WriteFile(bin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}

	var apiHits, dlHits atomic.Int32
	var sawAuth atomic.Bool
	var dlPath atomic.Value
	api := fakeAPIServer(t, tag, &apiHits, &sawAuth)
	t.Cleanup(api.Close)
	dl := fakeDownloadServer(t, serveBody, &dlHits, &dlPath)
	t.Cleanup(dl.Close)

	prevAPI, prevDL, prevBin, prevRestart := latestAPI, latestDownload, binaryPath, restart
	latestAPI, latestDownload = api.URL, dl.URL+"/"
	binaryPath = func() string { return bin }
	restarted := &[]string{}
	restart = func(path string) error {
		*restarted = append(*restarted, path)
		return nil
	}
	t.Cleanup(func() {
		latestAPI, latestDownload, binaryPath, restart = prevAPI, prevDL, prevBin, prevRestart
	})

	u := New(testLogger(), "0.1.45", token)
	u.checkAndMaybeRestart()
	return stageOut{bin: bin, restarted: restarted, dlHits: &dlHits, dlPath: &dlPath}
}

func TestCheckSwapsAndRestarts(t *testing.T) {
	out := stage(t, "9.9.9", "tok", fakeBinaryNew)

	got, err := os.ReadFile(out.bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != fakeBinaryNew {
		t.Fatalf("binary not swapped: %q", string(got))
	}
	bak, err := os.ReadFile(out.bin + ".bak")
	if err != nil {
		t.Fatalf("backup: %v", err)
	}
	if string(bak) != "old-binary" {
		t.Fatalf("backup = %q, want old content", string(bak))
	}
	if len(*out.restarted) != 1 || (*out.restarted)[0] != out.bin {
		t.Fatalf("restart = %v, want [%s]", *out.restarted, out.bin)
	}
	// The tag and the asset must come from the same release.
	if p, _ := out.dlPath.Load().(string); !strings.Contains(p, "/9.9.9/") {
		t.Fatalf("download path %q not pinned to the tag", p)
	}
	// Leftover temp files mean a leak on some path.
	leftovers, _ := filepath.Glob(filepath.Dir(out.bin) + "/.couch-pi-new-*")
	if len(leftovers) != 0 {
		t.Fatalf("temp file leak: %v", leftovers)
	}
}

func TestCheckSkipsOlderTag(t *testing.T) {
	out := stage(t, "0.0.1", "", fakeBinaryNew)
	if len(*out.restarted) != 0 {
		t.Fatalf("restarted on older tag")
	}
	if out.dlHits.Load() != 0 {
		t.Fatalf("downloaded on older tag (%d hits)", out.dlHits.Load())
	}
}

func TestCheckAbortsWhenProbeFails(t *testing.T) {
	out := stage(t, "9.9.9", "", "#!/bin/sh\necho nope\n")

	got, err := os.ReadFile(out.bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-binary" {
		t.Fatalf("binary swapped despite bad probe: %q", string(got))
	}
	if len(*out.restarted) != 0 {
		t.Fatalf("restarted despite bad probe")
	}
	leftovers, _ := filepath.Glob(filepath.Dir(out.bin) + "/.couch-pi-new-*")
	if len(leftovers) != 0 {
		t.Fatalf("temp file leak after failed probe: %v", leftovers)
	}
}

// The restart path is resolved once at New, before any swap, so a
// replacement of the running file can never point the exec at a
// "(deleted)" proc path.
func TestRestartPathResolvedOnce(t *testing.T) {
	out := stage(t, "9.9.9", "", fakeBinaryNew)
	binaryPath = func() string { return out.bin }
	u := New(testLogger(), "0.1.45", "")
	binaryPath = func() string { return "/gone/couch-pi (deleted)" }
	prevRestart := restart
	rec := &[]string{}
	restart = func(path string) error { *rec = append(*rec, path); return nil }
	t.Cleanup(func() { restart = prevRestart })

	u.checkAndMaybeRestart()
	if len(*rec) != 1 || (*rec)[0] != out.bin {
		t.Fatalf("restart path = %v, want the New-time path %s", *rec, out.bin)
	}
}

func TestCheckAbortsWhenBackupFails(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "couch-pi")
	if err := os.WriteFile(bin, []byte("old-binary"), 0o755); err != nil {
		t.Fatal(err)
	}
	// A directory at the .bak path makes the backup copy fail.
	if err := os.Mkdir(bin+".bak", 0o700); err != nil {
		t.Fatal(err)
	}
	var dlHits atomic.Int32
	var dlPath atomic.Value
	dl := fakeDownloadServer(t, fakeBinaryNew, &dlHits, &dlPath)
	t.Cleanup(dl.Close)
	var apiHits atomic.Int32
	var sawAuth atomic.Bool
	api := fakeAPIServer(t, "9.9.9", &apiHits, &sawAuth)
	t.Cleanup(api.Close)

	prevAPI, prevDL, prevBin, prevRestart := latestAPI, latestDownload, binaryPath, restart
	latestAPI, latestDownload = api.URL, dl.URL+"/"
	binaryPath = func() string { return bin }
	restart = func(string) error { return nil }
	t.Cleanup(func() {
		latestAPI, latestDownload, binaryPath, restart = prevAPI, prevDL, prevBin, prevRestart
	})

	u := New(testLogger(), "0.1.45", "")
	if _, err := u.check(); err == nil {
		t.Fatal("check succeeded despite backup failure")
	}
	got, err := os.ReadFile(bin)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "old-binary" {
		t.Fatalf("binary touched despite backup failure: %q", string(got))
	}
	leftovers, _ := filepath.Glob(dir + "/.couch-pi-new-*")
	if len(leftovers) != 0 {
		t.Fatalf("temp file leak on backup failure: %v", leftovers)
	}
}

// A probe that fails with "text file busy" is retried, then succeeds.
func TestProbeRetriesBusy(t *testing.T) {
	prev := probeBin
	calls := 0
	probeBin = func(string) ([]byte, error) {
		calls++
		if calls < 3 {
			return nil, &mockBusy{}
		}
		return []byte("9.9.9\n"), nil
	}
	t.Cleanup(func() { probeBin = prev })

	if err := probeWithRetries("x", "9.9.9"); err != nil {
		t.Fatalf("probe gave up: %v", err)
	}
	if calls != 3 {
		t.Fatalf("calls = %d, want 3", calls)
	}
}

func TestProbeRejectsWrongVersion(t *testing.T) {
	prev := probeBin
	probeBin = func(string) ([]byte, error) { return []byte("nope\n"), nil }
	t.Cleanup(func() { probeBin = prev })

	if err := probeWithRetries("x", "9.9.9"); err == nil {
		t.Fatal("wrong probe version accepted")
	}
}

// A stale token must not break a public lookup.
func TestLatestTagRetriesWithoutToken(t *testing.T) {
	attempts := atomic.Int32{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) == 1 {
			if r.Header.Get("Authorization") == "" {
				t.Error("first request should carry the token")
			}
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.Header.Get("Authorization") != "" {
			t.Error("retry should drop the token")
		}
		_, _ = w.Write([]byte(`{"tag_name":"9.9.9"}`))
	}))
	t.Cleanup(srv.Close)

	prevAPI := latestAPI
	latestAPI = srv.URL
	t.Cleanup(func() { latestAPI = prevAPI })

	u := New(testLogger(), "0.1.45", "tok")
	tag, err := u.latestTag()
	if err != nil {
		t.Fatal(err)
	}
	if tag != "9.9.9" {
		t.Fatalf("tag = %q", tag)
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
		{"v2.0.0", "1.9.9", 1},
	}
	for _, c := range cases {
		if got := cmpVersion(c.a, c.b); got != c.want {
			t.Errorf("cmpVersion(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

type mockBusy struct{}

func (*mockBusy) Error() string { return "fork/exec: text file busy" }

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
