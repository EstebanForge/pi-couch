package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"regexp"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// pi's TUI intercepts "/model <pattern>" and "/thinking <level>" in the
// editor before anything reaches the LLM (interactive-mode.js), and herdr
// prompts submit through that editor, so sending them as prompts switches
// the session mid-flight. Queued while working, applied at the next turn
// boundary.

// modelPattern accepts pi model patterns: provider/id, fuzzy fragments,
// and the optional ":thinking" suffix. Newlines would inject a second
// editor line.
var modelPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9/._:-]{0,119}$`)

var thinkingLevels = map[string]bool{
	"off": true, "minimal": true, "low": true, "medium": true,
	"high": true, "xhigh": true, "max": true,
}

var (
	errBadModel     = errors.New("bad model pattern")
	errModelFail    = errors.New("model set failed")
	errBadThinking  = errors.New("bad thinking level")
	errThinkingFail = errors.New("thinking set failed")
)

// apiModel and apiThinking are the JSON twins of modelSet/thinkingSet:
// fetch cannot read their 303 redirects, so outcomes are in-band.
func (a *app) apiModel(w http.ResponseWriter, r *http.Request) {
	if !a.requireHerdr(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 4<<10)
	var in struct {
		Pattern string `json:"pattern"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "badrequest"})
		return
	}
	a.writeModelOutcome(w, a.modelAgent(r.PathValue("key"), in.Pattern))
}

func (a *app) apiThinking(w http.ResponseWriter, r *http.Request) {
	if !a.requireHerdr(w) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<10)
	var in struct {
		Level string `json:"level"`
	}
	if err := json.NewDecoder(r.Body).Decode(&in); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "badrequest"})
		return
	}
	a.writeModelOutcome(w, a.thinkingAgent(r.PathValue("key"), in.Level))
}

func (a *app) writeModelOutcome(w http.ResponseWriter, err error) {
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
	case errors.Is(err, errBadModel), errors.Is(err, errBadThinking):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid"})
	case errors.Is(err, errNotFound):
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
	case errors.Is(err, herdr.ErrNotDriven):
		writeJSON(w, http.StatusConflict, map[string]string{"error": "notdriven"})
	default:
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "failed"})
	}
}

// modelAgent drives both transports; errors map to notices (templ)
// or in-band JSON codes (fetch).
func (a *app) modelAgent(key, pattern string) error {
	if !modelPattern.MatchString(pattern) {
		return errBadModel
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		return errNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, err := a.statusFor().TargetForPath(ctx, meta.Path)
	if err != nil {
		return herdr.ErrNotDriven
	}
	if err := a.herdr.Prompt(ctx, target, "/model "+pattern); err != nil {
		a.logger.Error("model set", "err", err)
		return errModelFail
	}
	return nil
}

func (a *app) modelSet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	back := "/sessions/" + url.PathEscape(key)
	err := a.modelAgent(key, r.FormValue("pattern"))
	switch {
	case err == nil:
		http.Redirect(w, r, back+"?modelset=1", http.StatusSeeOther)
	case errors.Is(err, errBadModel):
		http.Redirect(w, r, back+"?err=badmodel", http.StatusSeeOther)
	case errors.Is(err, errNotFound):
		http.NotFound(w, r)
	case errors.Is(err, herdr.ErrNotDriven):
		http.Redirect(w, r, back+"?err=notdriven", http.StatusSeeOther)
	default:
		http.Redirect(w, r, back+"?err=modelfail", http.StatusSeeOther)
	}
}

// thinkingAgent is the level twin of modelAgent.
func (a *app) thinkingAgent(key, level string) error {
	if !thinkingLevels[level] {
		return errBadThinking
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		return errNotFound
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	target, err := a.statusFor().TargetForPath(ctx, meta.Path)
	if err != nil {
		return herdr.ErrNotDriven
	}
	if err := a.herdr.Prompt(ctx, target, "/thinking "+level); err != nil {
		a.logger.Error("thinking set", "err", err)
		return errThinkingFail
	}
	return nil
}

func (a *app) thinkingSet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	back := "/sessions/" + url.PathEscape(key)
	err := a.thinkingAgent(key, r.FormValue("level"))
	switch {
	case err == nil:
		http.Redirect(w, r, back+"?thinkingset=1", http.StatusSeeOther)
	case errors.Is(err, errBadThinking):
		http.Redirect(w, r, back+"?err=badthinking", http.StatusSeeOther)
	case errors.Is(err, errNotFound):
		http.NotFound(w, r)
	case errors.Is(err, herdr.ErrNotDriven):
		http.Redirect(w, r, back+"?err=notdriven", http.StatusSeeOther)
	default:
		http.Redirect(w, r, back+"?err=thinkingfail", http.StatusSeeOther)
	}
}
