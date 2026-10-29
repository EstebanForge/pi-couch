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
	// The hub's argv is [bin, terminal, session, observe, pane, --cols,
	// C, --rows, R], so $4 is the pane id ($0 is this script): one pid
	// file per pane, or two spawns race their $$ writes into one file
	// and waitGone checks whichever pane lost.
	src := "#!/usr/bin/env bash\necho $$ > " + dir + "/lastpid-$4\n" + body + "\nexec sleep 60\n"
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
		// A killed process lingers under /proc as a zombie until its
		// parent reaps it, and reap scheduling under -race can outlast
		// this deadline. State Z means the exit landed, which is what
		// this check verifies.
		if sb, err := os.ReadFile(fmt.Sprintf("/proc/%s/stat", pid)); err == nil {
			if i := strings.LastIndex(string(sb), ")"); i >= 0 && i+2 < len(sb) && sb[i+2] == 'Z' {
				return
			}
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
	_, unsub3 := h.subscribe("p2", 80, 24)
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
		t.Fatalf("spawns = %d, want 2 (one per pane while no one joins late)", n)
	}
	unsub1()
	unsub3()
	waitGone(t, filepath.Join(dir, "lastpid-p1"))
	waitGone(t, filepath.Join(dir, "lastpid-p2"))
}

// herdr's observe emits its full repaint only on frame 0, so a viewer
// joining a running stream would get deltas against an empty grid. A
// late joiner respawns the process: everyone gets frame 0 again, and
// the new viewer's geometry wins.
func TestObserveLateJoinerGetsFullRepaint(t *testing.T) {
	dir := t.TempDir()
	// argv: $4 pane, $6 cols, $8 rows - the payload names its generation,
	// computed by the shell so each respawn reports its own dims.
	bin := observeScript(t, dir,
		"printf '{\"type\":\"terminal.frame\",\"encoding\":\"ansi\",\"bytes\":\"%s\"}\\n' \"$(printf 'F-%sx%s' \"$6\" \"$8\" | base64 | tr -d '\\n')\"\n"+
			"for i in $(seq 1 100); do sleep 0.05; "+frameLine(b64("tick"))+"; done")
	old := observeBin
	observeBin = bin
	t.Cleanup(func() { observeBin = old })

	h := newObserveHub("")
	ch1, unsub1 := h.subscribe("p1", 100, 30)
	defer unsub1()
	first := <-ch1
	if got, _ := base64.StdEncoding.DecodeString(first.bytes); string(got) != "F-100x30" {
		t.Fatalf("first viewer payload %q", got)
	}

	ch2, unsub2 := h.subscribe("p1", 200, 50)
	defer unsub2()
	select {
	case f := <-ch2:
		got, _ := base64.StdEncoding.DecodeString(f.bytes)
		if string(got) != "F-200x50" {
			t.Fatalf("late joiner first frame = %q, want a full repaint at its own dims", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("late joiner never got a frame")
	}
	// The early viewer shares the new generation's repaint.
	select {
	case f := <-ch1:
		got, _ := base64.StdEncoding.DecodeString(f.bytes)
		if string(got) != "F-200x50" {
			t.Fatalf("early viewer after respawn = %q", got)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("early viewer starved after respawn")
	}
}

// A stalled viewer's full buffer must evict it with a reset (and a
// closed stream, so the client reconnects) instead of silently losing
// frames and rendering a hole-riddled screen forever.
func TestObserveStalledViewerEvicted(t *testing.T) {
	dir := t.TempDir()
	line := `{"type":"terminal.frame","encoding":"ansi","bytes":"` + b64("spam") + `"}`
	awkLine := strings.ReplaceAll(line, `"`, `\"`)
	bin := observeScript(t, dir, `awk 'BEGIN{ for (i = 0; i < 400; i++) print "`+awkLine+`" }'`)
	old := observeBin
	observeBin = bin
	t.Cleanup(func() { observeBin = old })

	h := newObserveHub("")
	ch, unsub := h.subscribe("p1", 100, 30)
	defer unsub()

	// Consume nothing while the flood fills the buffer (64): the hub
	// must evict this viewer on its own, then close the channel.
	time.Sleep(500 * time.Millisecond)
	deadline := time.After(5 * time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return // evicted and closed
			}
		case <-deadline:
			t.Fatal("stalled viewer was never evicted")
		}
	}
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
