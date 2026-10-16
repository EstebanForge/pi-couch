package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The bridge dial carries a per-dialog id the extension generates at
// ui_prompt_start; the answer endpoint refuses keys unless the id still
// matches, so a stale card cannot fire into a replaced dialog.
func TestBridgeDialogCarriesID(t *testing.T) {
	a := &app{logger: testLogger()}
	start := func(id string) {
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/dialog", strings.NewReader(
			`{"phase":"start","kind":"confirm","title":"Allow edit?","id":"`+id+`","session_path":"/s/a.jsonl"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.apiBridgeDialog(rec, req)
		if rec.Code != http.StatusNoContent {
			t.Fatalf("start: code=%d, want 204", rec.Code)
		}
	}
	end := func(id string) {
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/dialog", strings.NewReader(
			`{"phase":"end","id":"`+id+`","session_path":"/s/a.jsonl"}`))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.apiBridgeDialog(rec, req)
	}

	start("d1")
	d, ok := a.bridgeDialog("/s/a.jsonl")
	if !ok || d.ID != "d1" || d.Kind != "confirm" {
		t.Fatalf("bridgeDialog = %+v ok=%v", d, ok)
	}

	// A replaced dialog overwrites the id; the old end must not clear it.
	start("d2")
	end("d1")
	if _, ok := a.bridgeDialog("/s/a.jsonl"); !ok {
		t.Fatal("end with a stale id must not clear the replacement")
	}
	// The matching end clears; so does an id-less end (older extension).
	end("d2")
	if _, ok := a.bridgeDialog("/s/a.jsonl"); ok {
		t.Fatal("matching end must clear the dialog")
	}
	start("d3")
	end("")
	if _, ok := a.bridgeDialog("/s/a.jsonl"); ok {
		t.Fatal("id-less end must clear the dialog")
	}
}

// The pill only shows while herdr reports blocked: the bridge dial and
// herdr's blocked mapping come from the same ui_prompt events, so a
// dialog without the badge is a stale entry whose end event was lost.
func TestDialogForGatesOnBlocked(t *testing.T) {
	a := &app{logger: testLogger()}
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge["/s/a.jsonl"] = bridgeEntry{
		dialog:  BridgeDialog{Kind: "confirm", Title: "Allow?", ID: "x"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()

	if _, ok := a.dialogFor("/s/a.jsonl", "working"); ok {
		t.Error("dialog must be gated off while the badge is not blocked")
	}
	d, ok := a.dialogFor("/s/a.jsonl", "blocked")
	if !ok || d.ID != "x" {
		t.Errorf("dialogFor blocked = %+v ok=%v", d, ok)
	}
	if _, ok := a.dialogFor("/s/none.jsonl", "blocked"); ok {
		t.Error("no entry must stay false")
	}
}

// The answer endpoint refuses keys unless everything still lines up:
// bridge entry with a matching id, a live driver, and pi's confirm
// hint on the pane. Every refusal answers 409 changed in-band.
func TestDialogAnswerValidation(t *testing.T) {
	a, sub, h := streamTestApp(t)
	sessPath := filepath.Join(sub, "s1.jsonl")

	answer := func(path, body string) (int, map[string]any) {
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		return rec.Code, out
	}

	// Bad bodies.
	if code, _ := answer("/api/sessions/s1/dialog/answer", `{`); code != http.StatusBadRequest {
		t.Errorf("bad json: code=%d, want 400", code)
	}
	if code, _ := answer("/api/sessions/s1/dialog/answer", `{"id":"x","answer":""}`); code != http.StatusBadRequest {
		t.Errorf("empty answer: code=%d, want 400", code)
	}
	// Any non-empty answer is a potential select option now, so an
	// unknown word passes the body check and dies on the bridge entry
	// instead.
	if code, body := answer("/api/sessions/s1/dialog/answer", `{"id":"x","answer":"maybe"}`); code != http.StatusConflict || body["error"] != "changed" {
		t.Errorf("bad answer: code=%d body=%v, want 409 changed", code, body)
	}
	// Unknown session.
	if code, _ := answer("/api/sessions/nope/dialog/answer", `{"id":"x","answer":"yes"}`); code != http.StatusNotFound {
		t.Errorf("unknown key: code=%d, want 404", code)
	}

	// No bridge entry: 409 before anything touches herdr.
	if code, body := answer("/api/sessions/s1/dialog/answer", `{"id":"x","answer":"yes"}`); code != http.StatusConflict || body["error"] != "changed" {
		t.Errorf("no entry: code=%d body=%v, want 409 changed", code, body)
	}

	// Id mismatch: 409.
	a.bridgeMu.Lock()
	a.bridge = map[string]bridgeEntry{}
	a.bridge[sessPath] = bridgeEntry{
		dialog:  BridgeDialog{Kind: "confirm", Title: "Allow?", ID: "real"},
		expires: time.Now().Add(time.Minute),
	}
	a.bridgeMu.Unlock()
	if code, body := answer("/api/sessions/s1/dialog/answer", `{"id":"x","answer":"yes"}`); code != http.StatusConflict || body["error"] != "changed" {
		t.Errorf("id mismatch: code=%d body=%v, want 409 changed", code, body)
	}

	// Matching id but no driver runs the fixture path: still 409, and
	// deterministically so on every machine.
	if code, body := answer("/api/sessions/s1/dialog/answer", `{"id":"real","answer":"yes"}`); code != http.StatusConflict || body["error"] != "changed" {
		t.Errorf("no driver: code=%d body=%v, want 409 changed", code, body)
	}

	// A refused answer must not hold the memo: that attempt claimed
	// "real", so a corrected retry (driver back, screen still asking)
	// must reach the send path again.
	if !a.answered.Claim("real") {
		t.Fatal("refused answers must release their claim")
	}
}
