package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// The home cards render the live driver state: the pane title and the
// workspace label ride next to the status, joined by session path.
// A driver status flip with no jsonl write must still reach clients:
// dialogs open and herdr flips blocked without touching session files,
// and the home list has to show it on the next tick.
func TestSessionsStreamStatusOnlyChange(t *testing.T) {
	var sends int32
	var sessPath string
	status := "working"
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, status: &status})
	_, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stream/sessions")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	frames := readFrames(t, resp.Body)

	waitFrame(t, frames, 5*time.Second, "reset")
	status = "blocked"
	f := waitFrame(t, frames, 8*time.Second, "reset")
	var patch sessionPatch
	if err := json.Unmarshal([]byte(f.data), &patch); err != nil {
		t.Fatal(err)
	}
	if patch.Badges == nil && patch.Driven == nil {
		t.Fatalf("status-only flip sent no map fields: %s", f.data)
	}
}

// Each stream owns its wake channel: one shared capacity-1 channel
// delivered to a single ready subscriber, so a second open stream
// waited out the whole tick after a dialog opened.
func TestDialogWakeReachesEverySubscriber(t *testing.T) {
	a := &app{}
	w1, un1 := a.subscribeWake()
	w2, un2 := a.subscribeWake()
	a.wakeDialog()
	for i, ch := range []<-chan struct{}{w1, w2} {
		select {
		case <-ch:
		default:
			t.Fatalf("subscriber %d missed the wake", i+1)
		}
	}
	un1()
	a.wakeDialog()
	select {
	case <-w2:
	default:
		t.Fatal("live subscriber missed the second wake")
	}
	_ = un2
}

func TestSessionsCarriesDrivenInfo(t *testing.T) {
	var sends int32
	var sessPath string
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends})
	a, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")

	req := httptest.NewRequest(http.MethodGet, "/api/sessions", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code=%d, want 200", rec.Code)
	}
	var out sessionsWire
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode: %v", err)
	}
	d, ok := out.Driven[sessPath]
	if !ok {
		t.Fatalf("no driven entry for %s", sessPath)
	}
	if d.Status != "blocked" {
		t.Errorf("status=%q, want blocked", d.Status)
	}
	if d.Title != "pi - s1:running..." {
		t.Errorf("title=%q", d.Title)
	}
	if d.Label != "Forge" {
		t.Errorf("label=%q", d.Label)
	}
	_ = sub
	_ = a
}

// Entry-delta frames: the sessions stream sends only what changed per
// tick — new or touched sessions ride upserts, gone ones ride removed —
// and the herdr maps still ride only on their own bytes.
func TestSessionsStreamFieldDelta(t *testing.T) {
	var sends int32
	var sessPath string
	status := "blocked"
	sock := herdrFake(t, fakeHerdrOpts{sessPath: &sessPath, sends: &sends, status: &status})
	a, sub, h := streamTestAppWithHerdr(t, sock)
	sessPath = filepath.Join(sub, "s1.jsonl")
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stream/sessions")
	if err != nil {
		t.Fatal(err)
	}
	frames := readFrames(t, resp.Body)
	defer resp.Body.Close()

	keys := func(f frame) map[string]bool {
		var m map[string]json.RawMessage
		if err := json.Unmarshal([]byte(f.data), &m); err != nil {
			t.Fatalf("decode %s: %v", f.data, err)
		}
		out := map[string]bool{}
		for k := range m {
			out[k] = true
		}
		return out
	}

	f := waitFrame(t, frames, 5*time.Second, "reset")
	k := keys(f)
	for _, want := range []string{"sessions", "badges", "driven", "awaiting", "total"} {
		if !k[want] {
			t.Fatalf("first frame missing %s: %s", want, f.data)
		}
	}

	// A new session file arrives as one upsert, not a whole list.
	writeLines(t, filepath.Join(sub, "s2.jsonl"), sessHeader)
	f = waitFrame(t, frames, 8*time.Second, "reset")
	k = keys(f)
	for _, gone := range []string{"sessions", "badges", "driven", "awaiting"} {
		if k[gone] {
			t.Fatalf("upsert frame resent %s: %s", gone, f.data)
		}
	}
	if !k["total"] || !k["upserts"] {
		t.Fatalf("upsert frame missing total/upserts: %s", f.data)
	}
	var patch sessionPatch
	if err := json.Unmarshal([]byte(f.data), &patch); err != nil {
		t.Fatal(err)
	}
	if patch.Total != 2 || len(patch.Upserts) != 1 || patch.Upserts[0].Key != "s2" {
		t.Fatalf("upsert payload: total=%d upserts=%+v", patch.Total, patch.Upserts)
	}

	// A driver status flip rides the next file tick: badges and driven
	// come back, awaiting stays out.
	status = "working"
	writeLines(t, filepath.Join(sub, "s3.jsonl"), sessHeader)
	f = waitFrame(t, frames, 8*time.Second, "reset")
	k = keys(f)
	if !k["badges"] || !k["driven"] || !k["upserts"] {
		t.Fatalf("third frame missing driver fields: %s", f.data)
	}
	if k["awaiting"] || k["sessions"] {
		t.Fatalf("third frame resent unchanged fields: %s", f.data)
	}

	// A deleted session file rides removed with the client key.
	if err := os.Remove(filepath.Join(sub, "s2.jsonl")); err != nil {
		t.Fatal(err)
	}
	f = waitFrame(t, frames, 8*time.Second, "reset")
	if err := json.Unmarshal([]byte(f.data), &patch); err != nil {
		t.Fatal(err)
	}
	if patch.Total != 2 || len(patch.Removed) != 1 || patch.Removed[0] != "s2" {
		t.Fatalf("removed payload: total=%d removed=%v", patch.Total, patch.Removed)
	}
	_ = a
}

// A tick that changes more entries than the upsert cap collapses into a
// full reset frame instead of a giant delta.
func TestSessionsStreamUpsertReset(t *testing.T) {
	_, sub, h := streamTestApp(t)
	srv := httptest.NewServer(h)
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/stream/sessions")
	if err != nil {
		t.Fatal(err)
	}
	frames := readFrames(t, resp.Body)
	defer resp.Body.Close()

	f := waitFrame(t, frames, 5*time.Second, "reset")
	var first sessionPatch
	if err := json.Unmarshal([]byte(f.data), &first); err != nil {
		t.Fatal(err)
	}
	if len(first.Sessions) != 1 {
		t.Fatalf("first frame sessions=%d", len(first.Sessions))
	}

	for i := range sessionUpsertCap + 1 {
		writeLines(t, filepath.Join(sub, "bulk-"+strconv.Itoa(i)+".jsonl"), sessHeader)
	}
	f = waitFrame(t, frames, 10*time.Second, "reset")
	var patch sessionPatch
	if err := json.Unmarshal([]byte(f.data), &patch); err != nil {
		t.Fatal(err)
	}
	if len(patch.Sessions) != sessionUpsertCap+2 {
		t.Fatalf("reset frame sessions=%d, want %d", len(patch.Sessions), sessionUpsertCap+2)
	}
	if len(patch.Upserts) != 0 || len(patch.Removed) != 0 {
		t.Fatalf("reset frame carried deltas: %s", f.data)
	}
}
