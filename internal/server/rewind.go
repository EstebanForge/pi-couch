package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
	"github.com/EstebanForge/couch-pi/internal/pi"
)

// rewindVerdictWait bounds how long the phone waits for the agent-side
// bridge to confirm the branch. Test-overridable.
var rewindVerdictWait = 8 * time.Second

type rewindPending struct {
	entryID string
	verdict chan rewindResult
}

type rewindResult struct {
	OK     bool   `json:"ok"`
	Code   string `json:"code,omitempty"`
	Reason string `json:"reason,omitempty"`
}

// apiRewind rewinds the session to just before a sent user message
// (ADR-0010): the phone names a transcript seq, the server resolves
// the jsonl entry and fires /couchpi-rewind through the agent's
// prompt path; the bridge reports the branch verdict.
func (a *app) apiRewind(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var req struct {
		Seq int `json:"seq"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	// hc has no keys verb, so pi's desk refill could not be cleared and
	// the next typed prompt would append to the stale buffer: refuse
	// until hc grows one (ADR-0010).
	if a.hcDrv != nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "unsupported-driver"})
		return
	}
	// Rewinding under a live turn would fork mid-stream: pi refuses it,
	// and the honest answer is the same busy the composer already shows.
	badges, _ := a.statusFor().DriveStatus(r.Context())
	switch badges[meta.Path] {
	case "working", "blocked":
		writeJSON(w, http.StatusConflict, map[string]string{"error": "busy"})
		return
	}
	_, items, err := a.idx.Transcript(key, 0)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	var target *pi.ViewItem
	for i := range items {
		if items[i].Seq == req.Seq {
			target = &items[i]
			break
		}
	}
	if target == nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "no such message"})
		return
	}
	if target.Kind != "user" || target.ID == "" {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "not a sent user message"})
		return
	}
	for _, it := range items {
		if it.Seq > target.Seq && it.Kind == "user" {
			writeJSON(w, http.StatusConflict, map[string]string{"error": "only the last sent message can be rewound"})
			return
		}
	}

	p := &rewindPending{entryID: target.ID, verdict: make(chan rewindResult, 1)}
	a.rewindMu.Lock()
	if a.rewindPends == nil {
		a.rewindPends = map[string]*rewindPending{}
	}
	if _, busy := a.rewindPends[key]; busy {
		a.rewindMu.Unlock()
		writeJSON(w, http.StatusConflict, map[string]string{"error": "a rewind is already in flight"})
		return
	}
	a.rewindPends[key] = p
	a.rewindMu.Unlock()

	dispatchErr := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), a.cmdBudget(25*time.Second, 10*time.Second))
		defer cancel()
		target, err := a.statusFor().TargetForPath(ctx, meta.Path)
		if err != nil {
			return herdr.ErrNotDriven
		}
		cmd := a.cmdFor()
		if cmd == nil {
			return herdr.ErrNotDriven
		}
		if err := cmd.Prompt(ctx, target, "/couchpi-rewind "+p.entryID); err != nil {
			return errPromptFail
		}
		return nil
	}()
	if dispatchErr != nil {
		a.rewindMu.Lock()
		if cur, ok := a.rewindPends[key]; ok && cur == p {
			delete(a.rewindPends, key)
		}
		a.rewindMu.Unlock()
		switch {
		case errors.Is(dispatchErr, herdr.ErrNotDriven):
			writeJSON(w, http.StatusConflict, map[string]string{"error": "notdriven"})
		case errors.Is(dispatchErr, errNotFound):
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		default:
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed"})
		}
		return
	}

	timer := time.NewTimer(rewindVerdictWait)
	defer timer.Stop()
	select {
	case res := <-p.verdict:
		if res.OK {
			// pi's desk refill puts the rewound text back in the pane
			// editor (interactive-mode's navigateTree action), so the next
			// typed prompt would append to it. One ctrl+c clears the buffer;
			// exiting pi needs two presses. hc has no keys verb.
			if a.hcDrv == nil && a.herdr != nil {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				if target, err := a.statusFor().TargetForPath(ctx, meta.Path); err == nil {
					if err := a.herdr.PaneSendInput(ctx, target, "", []string{"ctrl+c"}); err != nil {
						a.logger.Warn("rewind clear", "err", err)
					}
				}
			}
		}
		writeJSON(w, http.StatusOK, res)
	case <-timer.C:
		a.rewindMu.Lock()
		if cur, ok := a.rewindPends[key]; ok && cur == p {
			delete(a.rewindPends, key)
		}
		a.rewindMu.Unlock()
		writeJSON(w, http.StatusOK, rewindResult{Code: "timeout", Reason: "the agent didn't confirm the rewind - check the terminal before retrying"})
	}
}

// apiBridgeRewind takes the agent-side verdict for a rewind the phone
// is waiting on. Unknown or stale reports 204 so an old bridge or a
// restarted server never errors the extension.
func (a *app) apiBridgeRewind(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionPath string `json:"session_path"`
		EntryID     string `json:"entry_id"`
		OK          bool   `json:"ok"`
		Reason      string `json:"reason"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	a.rewindMu.Lock()
	defer a.rewindMu.Unlock()
	for key, p := range a.rewindPends {
		if p.entryID != req.EntryID {
			continue
		}
		meta, ok := a.idx.Meta(key)
		if !ok || meta.Path != req.SessionPath {
			continue
		}
		delete(a.rewindPends, key)
		res := rewindResult{OK: req.OK}
		if !req.OK {
			res.Code = "refused"
			res.Reason = req.Reason
		}
		p.verdict <- res
		w.WriteHeader(http.StatusNoContent)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
