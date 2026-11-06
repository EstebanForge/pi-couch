package server

import (
	"encoding/json"
	"math/rand"
	"net/http"
	"strconv"
	"time"
)

// The ask_user_question relay (ADR-0009): the bridge extension POSTs
// the open questionnaire here, polls for a phone answer, and POSTs the
// rpiv verdict back. The phone's POST parks a pending answer and
// returns the verdict, so a phone answer needs no keystrokes.

// askVerdictWait bounds the phone POST's wait for the extension round
// trip (poll tick + rpiv validation + verdict dial). Past it the
// answer's fate is unknown: the poll may still deliver it.
var askVerdictWait = 8 * time.Second

type askPhoneAnswer struct {
	ToolCallID string           `json:"tool_call_id"`
	Answers    []askAnswerEntry `json:"answers,omitempty"`
	Cancel     bool             `json:"cancel,omitempty"`
}

// askAnswerEntry mirrors rpiv's AskUserAnswerEntry minus notes (the
// phone card does not author notes; rpiv treats them as optional).
type askAnswerEntry struct {
	QuestionIndex int    `json:"question_index"`
	OptionIndexes []int  `json:"option_indexes,omitempty"`
	Text          string `json:"text,omitempty"`
}

type askAnswerWire struct {
	RequestID  string           `json:"request_id"`
	ToolCallID string           `json:"tool_call_id"`
	Answers    []askAnswerEntry `json:"answers,omitempty"`
	Cancel     bool             `json:"cancel,omitempty"`
}

// askAnswerResult is the phone POST's body; codes mirror rpiv's reject
// codes where one applies (invalid, not_found) plus timeout.
type askAnswerResult struct {
	OK     bool   `json:"ok"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

type askPending struct {
	requestID string
	wire      askAnswerWire
	delivered bool
	verdict   chan askAnswerResult
}

func (a *app) apiBridgeAsk(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Phase       string        `json:"phase"`
		ToolCallID  string        `json:"tool_call_id"`
		SessionPath string        `json:"session_path"`
		Questions   []AskQuestion `json:"questions,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil || req.SessionPath == "" || req.ToolCallID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}

	a.bridgeMu.Lock()
	if a.bridge == nil {
		a.bridge = map[string]bridgeEntry{}
	}
	switch req.Phase {
	case "open":
		// A new questionnaire replaces any previous one for the path.
		a.bridge[req.SessionPath] = bridgeEntry{
			dialog:  BridgeDialog{ID: req.ToolCallID, Kind: "ask", Questions: req.Questions},
			expires: time.Now().Add(bridgeTTL),
		}
	case "close":
		if e, ok := a.bridge[req.SessionPath]; ok && e.dialog.Kind == "ask" && e.dialog.ID == req.ToolCallID {
			delete(a.bridge, req.SessionPath)
		}
		a.dropAskPending(req.ToolCallID, askAnswerResult{Code: "not_found", Reason: "questionnaire closed"})
	default:
		a.bridgeMu.Unlock()
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	a.bridgeMu.Unlock()
	a.wakeDialog()
	w.WriteHeader(http.StatusNoContent)
}

// apiBridgeAskAnswer hands one pending phone answer to the extension
// poll, exactly once. 204 when nothing is pending (or already
// delivered): the poll just keeps ticking.
func (a *app) apiBridgeAskAnswer(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("tool_call_id")
	if id == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	a.askMu.Lock()
	defer a.askMu.Unlock()
	p, ok := a.askPends[id]
	if !ok || p.delivered {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	p.delivered = true
	writeJSON(w, http.StatusOK, p.wire)
}

// apiBridgeAskVerdict records the rpiv verdict for a pending answer.
// Unknown or already-resolved pendings stay 204: the phone POST either
// already returned or is on its own timeout path.
func (a *app) apiBridgeAskVerdict(w http.ResponseWriter, r *http.Request) {
	var req struct {
		RequestID  string `json:"request_id"`
		ToolCallID string `json:"tool_call_id"`
		OK         bool   `json:"ok"`
		Code       string `json:"code,omitempty"`
		Reason     string `json:"reason,omitempty"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&req); err != nil || req.ToolCallID == "" || req.RequestID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	a.askMu.Lock()
	defer a.askMu.Unlock()
	p, ok := a.askPends[req.ToolCallID]
	if !ok || p.requestID != req.RequestID {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	delete(a.askPends, req.ToolCallID)
	res := askAnswerResult{OK: req.OK, Code: req.Code, Reason: req.Reason}
	select {
	case p.verdict <- res:
	default:
	}
	w.WriteHeader(http.StatusNoContent)
}

// dropAskPending resolves and removes any pending answer for a tool
// call. Called from the ask close branch while bridgeMu is held; the
// inner askMu take keeps the lock order bridgeMu -> askMu everywhere.
func (a *app) dropAskPending(toolCallID string, result askAnswerResult) {
	a.askMu.Lock()
	defer a.askMu.Unlock()
	p, ok := a.askPends[toolCallID]
	if !ok {
		return
	}
	delete(a.askPends, toolCallID)
	select {
	case p.verdict <- result:
	default:
	}
}

// apiAskAnswer answers an open ask_user_question from the phone. The
// structured answer parks as a pending payload for the extension poll
// and the request returns the rpiv verdict; a timeout means the poll
// never came - the answer may still land, so the card keeps its copy.
func (a *app) apiAskAnswer(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var req askPhoneAnswer
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil || req.ToolCallID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	writeJSON(w, http.StatusOK, a.answerAskForPath(meta.Path, req))
}

// answerAskForPath is the ask answer core, session path addressed, so
// tests and the HTTP handler share one gate.
func (a *app) answerAskForPath(sessionPath string, req askPhoneAnswer) askAnswerResult {
	dialog, has := a.bridgeDialog(sessionPath)
	if !has || dialog.Kind != "ask" || dialog.ID != req.ToolCallID {
		return askAnswerResult{Code: "not_found", Reason: "no open questionnaire for that id"}
	}
	if !req.Cancel {
		if code := validateAskAnswers(dialog.Questions, req.Answers); code != "" {
			return askAnswerResult{Code: "invalid", Reason: code}
		}
	}

	p := &askPending{
		requestID: newAskRequestID(),
		wire: askAnswerWire{
			ToolCallID: req.ToolCallID,
			Answers:    req.Answers,
			Cancel:     req.Cancel,
		},
		verdict: make(chan askAnswerResult, 1),
	}
	p.wire.RequestID = p.requestID
	a.askMu.Lock()
	// Run builds the app without pre-seeding this map (tests seed it
	// by hand); first answer parks lazily, same as the bridge map.
	if a.askPends == nil {
		a.askPends = map[string]*askPending{}
	}
	if _, busy := a.askPends[req.ToolCallID]; busy {
		a.askMu.Unlock()
		return askAnswerResult{Code: "busy", Reason: "an answer is already in flight"}
	}
	a.askPends[req.ToolCallID] = p
	a.askMu.Unlock()

	timer := time.NewTimer(askVerdictWait)
	defer timer.Stop()
	select {
	case res := <-p.verdict:
		return res
	case <-timer.C:
		a.askMu.Lock()
		// A verdict that raced the timer must not be lost to the map
		// delete: dropAskPending-free removal only when still ours.
		if cur, ok := a.askPends[req.ToolCallID]; ok && cur == p {
			delete(a.askPends, req.ToolCallID)
		}
		a.askMu.Unlock()
		return askAnswerResult{Code: "timeout", Reason: "no verdict in time - the answer may still land"}
	}
}

// validateAskAnswers applies rpiv's shape rules cheaply so obviously
// malformed answers refuse without a round trip: every question
// answered exactly once, indexes in range, distinct, and each entry
// carries options or text. Returns a human reason, or "" when valid.
func validateAskAnswers(questions []AskQuestion, answers []askAnswerEntry) string {
	if len(questions) == 0 || len(answers) != len(questions) {
		return "every question needs exactly one answer"
	}
	seen := make(map[int]bool, len(answers))
	for _, ans := range answers {
		if ans.QuestionIndex < 0 || ans.QuestionIndex >= len(questions) || seen[ans.QuestionIndex] {
			return "question_index out of range or duplicated"
		}
		seen[ans.QuestionIndex] = true
		q := questions[ans.QuestionIndex]
		if len(ans.OptionIndexes) == 0 && ans.Text == "" {
			return "an answer needs options or text"
		}
		picked := make(map[int]bool, len(ans.OptionIndexes))
		for _, i := range ans.OptionIndexes {
			if i < 0 || i >= len(q.Options) || picked[i] {
				return "option index out of range or duplicated"
			}
			picked[i] = true
		}
		if !q.MultiSelect && len(ans.OptionIndexes) > 1 {
			return "single-select takes one option"
		}
		// rpiv's contract: single-select takes one of options or text,
		// never both.
		if !q.MultiSelect && len(ans.OptionIndexes) > 0 && ans.Text != "" {
			return "single-select takes options or text, not both"
		}
	}
	return ""
}

func newAskRequestID() string {
	return strconv.FormatInt(time.Now().UnixNano(), 36) + "-" + strconv.FormatInt(rand.Int63(), 36)
}
