package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// observeScript writes a fake `herdr` binary: a bash script that ignores
// the CLI args, records its own pid, emits body's stdout, then sleeps
// forever via exec so a kill reaps it cleanly.
func observeScript(t *testing.T, dir, body string) string {
	t.Helper()
	p := filepath.Join(dir, fmt.Sprintf("herdr-%d", time.Now().UnixNano()))
	src := "#!/usr/bin/env bash\necho $$ > " + dir + "/lastpid\n" + body + "\nexec sleep 60\n"
	if err := os.WriteFile(p, []byte(src), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func waitGone(t *testing.T, pidFile string) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		b, err := os.ReadFile(pidFile)
		if err != nil {
			t.Fatalf("pid file never written: %v", err)
		}
		pid := strings.TrimSpace(string(b))
		if pid == "" {
			return
		}
		if _, err := os.Stat(fmt.Sprintf("/proc/%s", pid)); os.IsNotExist(err) {
			return
		}
		time.Sleep(30 * time.Millisecond)
	}
	t.Fatal("observe process still alive after unsubscribe")
}

func b64(s string) string { return base64.StdEncoding.EncodeToString([]byte(s)) }

// frameLine renders one terminal.frame NDJSON record; base64 is
// shell-safe so the payloads go in as plain literals.
func frameLine(b string) string {
	return `printf '{"type":"terminal.frame","encoding":"ansi","bytes":"` + b + `"}\n'`
}

func TestObserveHubFansFrames(t *testing.T) {
	dir := t.TempDir()
	bin := observeScript(t, dir, frameLine(b64("hello"))+"\n"+frameLine(b64(" world")))
	old := observeBin
	observeBin = bin
	t.Cleanup(func() { observeBin = old })

	h := newObserveHub("")
	ch, unsub := h.subscribe("p1", 100, 30)
	defer unsub()

	select {
	case f := <-ch:
		if f.failed || f.closed {
			t.Fatalf("first frame failed=%v closed=%v", f.failed, f.closed)
		}
		got, err := base64.StdEncoding.DecodeString(f.bytes)
		if err != nil || string(got) != "hello" {
			t.Fatalf("first frame = %q (%v), want hello", f.bytes, err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no first frame")
	}
	select {
	case f := <-ch:
		got, _ := base64.StdEncoding.DecodeString(f.bytes)
		if string(got) != " world" {
			t.Fatalf("second frame = %q, want ' world'", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no second frame")
	}
}

func TestObserveHubOneSpawnPerPane(t *testing.T) {
	dir := t.TempDir()
	spawns := filepath.Join(dir, "spawns")
	// the emit line lets the test read a frame without waiting on the
	// process to die
	bin := observeScript(t, dir, "echo x >> "+spawns+"\n"+frameLine(b64("kick")))
	old := observeBin
	observeBin = bin
	t.Cleanup(func() { observeBin = old })

	h := newObserveHub("")
	ch1, unsub1 := h.subscribe("p1", 100, 30)
	ch2, unsub2 := h.subscribe("p1", 100, 30)
	_, unsub3 := h.subscribe("p2", 80, 24)
	// drain ch2 so the fan-out never blocks on a full buffer
	go func() {
		for range ch2 {
		}
	}()
	<-ch1
	deadline := time.Now().Add(2 * time.Second)
	for {
		b, err := os.ReadFile(spawns)
		if err == nil && strings.Count(string(b), "\n") >= 2 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("want 2 spawns (p1+p2), file so far: %q", b)
		}
		time.Sleep(30 * time.Millisecond)
	}
	if n := strings.Count(mustRead(t, spawns), "\n"); n != 2 {
		t.Fatalf("spawns = %d, want 2 (one per pane, two viewers share p1)", n)
	}
	unsub1()
	unsub2()
	waitGone(t, filepath.Join(dir, "lastpid"))
	unsub3()
	waitGone(t, filepath.Join(dir, "lastpid"))
}

func mustRead(t *testing.T, p string) string {
	t.Helper()
	b, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func TestStreamTerminalObserveFrames(t *testing.T) {
	dir := t.TempDir()
	bin := observeScript(t, dir, frameLine(b64("live")))
	oldBin := observeBin
	observeBin = bin
	t.Cleanup(func() { observeBin = oldBin })

	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath})
	_, sub, handler := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		srv.URL+"/stream/terminal/s1?cols=90&rows=24", nil)
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()

	frames := readFrames(t, res.Body)
	f := waitFrame(t, frames, 5*time.Second, "delta")
	var payload struct {
		Bytes string `json:"bytes"`
	}
	if err := json.Unmarshal([]byte(f.data), &payload); err != nil {
		t.Fatalf("delta payload unparsable: %v", err)
	}
	got, err := base64.StdEncoding.DecodeString(payload.Bytes)
	if err != nil || string(got) != "live" {
		t.Fatalf("delta payload = %q (%v), want live", payload.Bytes, err)
	}
}
