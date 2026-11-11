package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

const testAskID = "tc-1234"

// askOpen dials the extension's open leg and asserts 204.
func askOpen(t *testing.T, a *app, body string) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/bridge/ask", strings.NewReader(body))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiBridgeAsk(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("ask open: code=%d, want 204: %s", rec.Code, rec.Body.String())
	}
}

func testAskOpenBody() string {
	return `{"phase":"open","tool_call_id":"` + testAskID + `","session_path":"/s/a.jsonl","questions":[` +
		`{"question":"Which release channel?","header":"Deploy","multi_select":false,"options":[{"label":"GitHub Release","description":"self-updater"},{"label":"Manual install","description":"no tag"}]},` +
		`{"question":"Which tool types?","header":"Scope","multi_select":true,"options":[{"label":"Single","description":""},{"label":"Multi","description":""}]}` +
		`]}`
}

func TestBridgeAskOpenClose(t *testing.T) {
	a := &app{logger: testLogger()}
	askOpen(t, a, testAskOpenBody())

	d, ok := a.bridgeDialog("/s/a.jsonl")
	if !ok || d.Kind != "ask" || d.ID != testAskID {
		t.Fatalf("bridgeDialog = %+v ok=%v", d, ok)
	}
	if len(d.Questions) != 2 || d.Questions[0].Header != "Deploy" || !d.Questions[1].MultiSelect {
		t.Fatalf("questions = %+v", d.Questions)
	}

	askOpen(t, a, `{"phase":"close","tool_call_id":"`+testAskID+`","session_path":"/s/a.jsonl"}`)
	if _, ok := a.bridgeDialog("/s/a.jsonl"); ok {
		t.Error("ask must be gone after close")
	}
}

// A close for a replaced questionnaire must not clear the replacement.
func TestBridgeAskCloseIDGuard(t *testing.T) {
	a := &app{logger: testLogger()}
	askOpen(t, a, testAskOpenBody())
	askOpen(t, a, `{"phase":"close","tool_call_id":"tc-other","session_path":"/s/a.jsonl"}`)
	if _, ok := a.bridgeDialog("/s/a.jsonl"); !ok {
		t.Fatal("mismatched close must keep the ask")
	}
}

// The generic ui_prompt_start dial (kind=custom, no questions) must not
// downgrade a live ask entry, and the generic end (uuid id) must not
// clear it: the questionnaire's own close owns removal.
func TestBridgeAskStickyOverCustom(t *testing.T) {
	a := &app{logger: testLogger()}
	askOpen(t, a, testAskOpenBody())

	req := httptest.NewRequest(http.MethodPost, "/api/bridge/dialog", strings.NewReader(
		`{"phase":"start","kind":"custom","session_path":"/s/a.jsonl","id":"uuid-1"}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiBridgeDialog(rec, req)
	d, ok := a.bridgeDialog("/s/a.jsonl")
	if !ok || d.Kind != "ask" || len(d.Questions) != 2 {
		t.Fatalf("custom start downgraded the ask: %+v ok=%v", d, ok)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/bridge/dialog", strings.NewReader(
		`{"phase":"end","session_path":"/s/a.jsonl","id":"uuid-1"}`))
	req.Host = "127.0.0.1:8090"
	rec = httptest.NewRecorder()
	a.apiBridgeDialog(rec, req)
	if _, ok := a.bridgeDialog("/s/a.jsonl"); !ok {
		t.Fatal("generic end cleared the ask")
	}

	askOpen(t, a, `{"phase":"close","tool_call_id":"`+testAskID+`","session_path":"/s/a.jsonl"}`)
	if _, ok := a.bridgeDialog("/s/a.jsonl"); ok {
		t.Error("ask close must remove the entry")
	}
}

func TestBridgeAskOpenBadBody(t *testing.T) {
	a := &app{logger: testLogger()}
	for _, body := range []string{`{`, `{"phase":"open","session_path":"/s/a.jsonl"}`, `{"phase":"open","tool_call_id":"x"}`} {
		req := httptest.NewRequest(http.MethodPost, "/api/bridge/ask", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.apiBridgeAsk(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: code=%d, want 400", body, rec.Code)
		}
	}
}

// The poll hands the pending answer to the extension exactly once, and
// the verdict resolves the waiting phone POST.
func TestBridgeAskPollAndVerdict(t *testing.T) {
	a := &app{logger: testLogger()}
	a.askMu.Lock()
	a.askPends = map[string]*askPending{}
	a.askMu.Unlock()
	askOpen(t, a, testAskOpenBody())

	old := askVerdictWait
	askVerdictWait = 5 * time.Second
	t.Cleanup(func() { askVerdictWait = old })

	done := make(chan string, 1)
	go func() {
		res := a.answerAskForPath("/s/a.jsonl", askPhoneAnswer{
			ToolCallID: testAskID,
			Answers: []askAnswerEntry{
				{QuestionIndex: 0, OptionIndexes: []int{0}},
				{QuestionIndex: 1, OptionIndexes: []int{0, 1}},
			},
		})
		b, _ := json.Marshal(res)
		done <- string(b)
	}()

	time.Sleep(50 * time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/bridge/ask/answer?tool_call_id="+testAskID, nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiBridgeAskAnswer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("poll: code=%d, want 200", rec.Code)
	}
	var got askAnswerWire
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("poll body: %v", err)
	}
	if got.RequestID == "" || got.ToolCallID != testAskID || len(got.Answers) != 2 || got.Answers[1].OptionIndexes[1] != 1 {
		t.Fatalf("poll wire = %+v", got)
	}

	// Second poll: delivered once only.
	rec = httptest.NewRecorder()
	a.apiBridgeAskAnswer(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("second poll: code=%d, want 204", rec.Code)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/bridge/ask/verdict", strings.NewReader(
		`{"request_id":"`+got.RequestID+`","tool_call_id":"`+testAskID+`","ok":true}`))
	req.Host = "127.0.0.1:8090"
	rec = httptest.NewRecorder()
	a.apiBridgeAskVerdict(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("verdict: code=%d, want 204", rec.Code)
	}

	select {
	case out := <-done:
		if !strings.Contains(out, `"ok":true`) {
			t.Fatalf("phone POST result = %s", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("phone POST never resolved")
	}
}

// Production Run builds the app without pre-seeding askPends; tests
// seed it by hand. The first real phone answer must park cleanly
// anyway, not panic the handler into a proxy 502.
func TestBridgeAskAnswersWithoutASeededMap(t *testing.T) {
	a := &app{logger: testLogger()}
	askOpen(t, a, testAskOpenBody())

	old := askVerdictWait
	askVerdictWait = 5 * time.Second
	t.Cleanup(func() { askVerdictWait = old })

	done := make(chan string, 1)
	go func() {
		res := a.answerAskForPath("/s/a.jsonl", askPhoneAnswer{
			ToolCallID: testAskID,
			Answers: []askAnswerEntry{
				{QuestionIndex: 0, OptionIndexes: []int{1}},
				{QuestionIndex: 1, OptionIndexes: []int{0, 1}},
			},
		})
		b, _ := json.Marshal(res)
		done <- string(b)
	}()

	time.Sleep(50 * time.Millisecond)
	req := httptest.NewRequest(http.MethodGet, "/api/bridge/ask/answer?tool_call_id="+testAskID, nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiBridgeAskAnswer(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("poll: code=%d, want 200", rec.Code)
	}
	var got askAnswerWire
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("poll body: %v", err)
	}

	req = httptest.NewRequest(http.MethodPost, "/api/bridge/ask/verdict", strings.NewReader(
		`{"request_id":"`+got.RequestID+`","tool_call_id":"`+testAskID+`","ok":true}`))
	req.Host = "127.0.0.1:8090"
	rec = httptest.NewRecorder()
	a.apiBridgeAskVerdict(rec, req)

	select {
	case out := <-done:
		if !strings.Contains(out, `"ok":true`) {
			t.Fatalf("phone POST result = %s", out)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("phone POST never resolved")
	}
}

// A verdict for an unknown request id must not block or 500.
func TestBridgeAskVerdictUnknown(t *testing.T) {
	a := &app{logger: testLogger()}
	req := httptest.NewRequest(http.MethodPost, "/api/bridge/ask/verdict", strings.NewReader(
		`{"request_id":"nope","tool_call_id":"gone","ok":true}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiBridgeAskVerdict(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("code=%d, want 204", rec.Code)
	}
}

// No poll, no verdict: the phone POST resolves as timeout inside the
// server's own bound, never hanging the client.
func TestBridgeAskVerdictTimeout(t *testing.T) {
	a := &app{logger: testLogger()}
	a.askMu.Lock()
	a.askPends = map[string]*askPending{}
	a.askMu.Unlock()
	askOpen(t, a, testAskOpenBody())

	old := askVerdictWait
	askVerdictWait = 80 * time.Millisecond
	t.Cleanup(func() { askVerdictWait = old })

	start := time.Now()
	res := a.answerAskForPath("/s/a.jsonl", askPhoneAnswer{ToolCallID: testAskID, Cancel: true})
	if res.OK || res.Code != "timeout" {
		t.Fatalf("res = %+v, want timeout", res)
	}
	if time.Since(start) > 2*time.Second {
		t.Fatalf("timeout took %v", time.Since(start))
	}
}

// Closing the questionnaire while a phone POST waits resolves it as
// not_found - rpiv dropped the dialog, the answer can never apply.
func TestBridgeAskCloseResolvesPending(t *testing.T) {
	a := &app{logger: testLogger()}
	a.askMu.Lock()
	a.askPends = map[string]*askPending{}
	a.askMu.Unlock()
	askOpen(t, a, testAskOpenBody())

	old := askVerdictWait
	askVerdictWait = 5 * time.Second
	t.Cleanup(func() { askVerdictWait = old })

	done := make(chan askAnswerResult, 1)
	go func() {
		done <- a.answerAskForPath("/s/a.jsonl", askPhoneAnswer{ToolCallID: testAskID, Cancel: true})
	}()
	time.Sleep(50 * time.Millisecond)
	askOpen(t, a, `{"phase":"close","tool_call_id":"`+testAskID+`","session_path":"/s/a.jsonl"}`)

	select {
	case res := <-done:
		if res.OK || res.Code != "not_found" {
			t.Fatalf("res = %+v, want not_found", res)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("phone POST never resolved")
	}
}

func TestBridgeAskPhoneValidation(t *testing.T) {
	a := &app{logger: testLogger()}
	a.askMu.Lock()
	a.askPends = map[string]*askPending{}
	a.askMu.Unlock()
	askOpen(t, a, testAskOpenBody())

	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown tool call", `{"tool_call_id":"other","answers":[]}`, "not_found"},
		{"missing answers", `{"tool_call_id":"` + testAskID + `"}`, "invalid"},
		{"short answers", `{"tool_call_id":"` + testAskID + `","answers":[{"question_index":0,"option_indexes":[0]}]}`, "invalid"},
		{"bad question index", `{"tool_call_id":"` + testAskID + `","answers":[{"question_index":5,"option_indexes":[0]},{"question_index":0,"option_indexes":[0]}]}`, "invalid"},
		{"duplicate question", `{"tool_call_id":"` + testAskID + `","answers":[{"question_index":0,"option_indexes":[0]},{"question_index":0,"option_indexes":[1]}]}`, "invalid"},
		{"option out of range", `{"tool_call_id":"` + testAskID + `","answers":[{"question_index":0,"option_indexes":[7]},{"question_index":1,"option_indexes":[0]}]}`, "invalid"},
		{"empty entry", `{"tool_call_id":"` + testAskID + `","answers":[{"question_index":0},{"question_index":1,"text":"x"}]}`, "invalid"},
		{"duplicate option", `{"tool_call_id":"` + testAskID + `","answers":[{"question_index":0,"option_indexes":[1,1]},{"question_index":1,"option_indexes":[0]}]}`, "invalid"},
		{"multi option single-select", `{"tool_call_id":"` + testAskID + `","answers":[{"question_index":0,"option_indexes":[0,1]},{"question_index":1,"option_indexes":[0]}]}`, "invalid"},
		{"options plus text single-select", `{"tool_call_id":"` + testAskID + `","answers":[{"question_index":0,"option_indexes":[0],"text":"both"},{"question_index":1,"option_indexes":[0]}]}`, "invalid"},
	}
	for _, tc := range cases {
		var parsed struct {
			ToolCallID string           `json:"tool_call_id"`
			Answers    []askAnswerEntry `json:"answers"`
			Cancel     bool             `json:"cancel"`
		}
		if err := json.Unmarshal([]byte(tc.body), &parsed); err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		res := a.answerAskForPath("/s/a.jsonl", askPhoneAnswer{ToolCallID: parsed.ToolCallID, Answers: parsed.Answers, Cancel: parsed.Cancel})
		if res.OK || res.Code != tc.want {
			t.Errorf("%s: res = %+v, want %s", tc.name, res, tc.want)
		}
		// A refused answer must not leave a pending slot behind.
		a.askMu.Lock()
		n := len(a.askPends)
		a.askMu.Unlock()
		if n != 0 {
			t.Errorf("%s: %d pending left behind", tc.name, n)
		}
	}

	// A well-formed cancel passes validation and lands pending.
	old := askVerdictWait
	askVerdictWait = 60 * time.Millisecond
	t.Cleanup(func() { askVerdictWait = old })
	res := a.answerAskForPath("/s/a.jsonl", askPhoneAnswer{ToolCallID: testAskID, Cancel: true})
	if res.OK || res.Code != "timeout" {
		t.Fatalf("cancel res = %+v, want pending-then-timeout", res)
	}
}

// The handler refuses when the open ask is gone or the id moved on -
// same id-gate discipline as the keystroke answer endpoint.
func TestBridgeAskPhoneIDGate(t *testing.T) {
	a, sessPath := hcDialogApp(t)
	drv, _ := startHCFake(t, sessPath, confirmPaint("Allow?"))
	wireHC(a, drv)
	waitForHC(t, func() bool {
		_, err := a.hcDrv.TargetForPath(t.Context(), sessPath)
		return err == nil
	})
	h := a.guard(a.routes())
	a.askMu.Lock()
	a.askPends = map[string]*askPending{}
	a.askMu.Unlock()

	// No ask open at all: in-band refusal, not an HTTP error - the
	// result body carries the code either way.
	req := httptest.NewRequest(http.MethodPost, "/api/sessions/s1/ask/answer", strings.NewReader(
		`{"tool_call_id":"`+testAskID+`","cancel":true}`))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || strings.Contains(rec.Body.String(), `"ok":true`) {
		t.Fatalf("no ask: code=%d body=%s, want 200 ok:false", rec.Code, rec.Body.String())
	}

	// Ask open, but the phone names the wrong id.
	askOpen(t, a, strings.Replace(testAskOpenBody(), "/s/a.jsonl", sessPath, 1))
	req = httptest.NewRequest(http.MethodPost, "/api/sessions/s1/ask/answer", strings.NewReader(
		`{"tool_call_id":"tc-stale","cancel":true}`))
	req.Host = "127.0.0.1:8090"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), `"not_found"`) {
		t.Fatalf("stale id: code=%d body=%s, want 200 not_found", rec.Code, rec.Body.String())
	}

	// Unknown session key.
	req = httptest.NewRequest(http.MethodPost, "/api/sessions/none/ask/answer", strings.NewReader(
		`{"tool_call_id":"`+testAskID+`","cancel":true}`))
	req.Host = "127.0.0.1:8090"
	rec = httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("unknown key: code=%d, want 404", rec.Code)
	}
}

// pi never flips its 7501 status to blocked while a questionnaire
// waits (hc findings), so the ask entry must surface on its own
// lifetime - the close POST and the entry TTL bound it, not the badge.
func TestDialogForSurfacesAskWithoutBlockedBadge(t *testing.T) {
	pane := "Deploy?\n[ ] GitHub Release\n[ ] Manual install"
	a, sessPath := hcDialogApp(t)
	drv, _ := startHCFake(t, sessPath, pane)
	wireHC(a, drv)
	a.awaiting.tick(t.Context())

	askOpen(t, a, strings.Replace(testAskOpenBody(), "/s/a.jsonl", sessPath, 1))
	d, ok := a.dialogFor(sessPath, "working")
	if !ok || d.Kind != "ask" || d.ID != testAskID {
		t.Fatalf("dialogFor = %+v ok=%v", d, ok)
	}
}

// The ask wire skips the pane parse: questions are the truth, and the
// rpiv overlay must not leak checkbox rows into the card.
func TestDialogWireAskSkipsPaneParse(t *testing.T) {
	pane := "Deploy?\n[ ] GitHub Release\n[ ] Manual install"
	a, sessPath := hcDialogApp(t)
	drv, _ := startHCFake(t, sessPath, pane)
	wireHC(a, drv)
	a.awaiting.tick(t.Context())

	askOpen(t, a, strings.Replace(testAskOpenBody(), "/s/a.jsonl", sessPath, 1))
	d, ok := a.dialogFor(sessPath, "blocked")
	if !ok || d.Kind != "ask" {
		t.Fatalf("dialogFor = %+v ok=%v", d, ok)
	}
	w := a.dialogWireFor(sessPath, d, true)
	if w == nil || w.Kind != "ask" || len(w.Questions) != 2 {
		t.Fatalf("wire = %+v", w)
	}
	if w.Title != "Deploy" {
		t.Errorf("title = %q, want the first question header", w.Title)
	}
	if len(w.Options) != 0 || w.Body != "" {
		t.Errorf("pane parse leaked into the ask wire: %+v", w)
	}
}

func TestDialogEqualQuestions(t *testing.T) {
	ask := func() *dialogWire {
		return &dialogWire{ID: testAskID, Kind: "ask", Title: "Deploy", Questions: []AskQuestion{
			{Question: "q", Header: "H", Options: []AskOption{{Label: "a"}, {Label: "b"}}},
		}}
	}
	if !dialogEqual(ask(), ask()) {
		t.Error("identical asks must compare equal")
	}
	mutated := ask()
	mutated.Questions[0].Options = mutated.Questions[0].Options[:1]
	if dialogEqual(ask(), mutated) {
		t.Error("dropping an option must count as a change")
	}
	plain := ask()
	plain.Questions = nil
	if dialogEqual(ask(), plain) {
		t.Error("losing the questions must count as a change")
	}
}
