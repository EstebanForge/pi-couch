package server

import (
	"context"
	"net/http"
	"net/url"
	"regexp"
	"time"
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

func (a *app) modelSet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	back := "/sessions/" + url.PathEscape(key)
	pattern := r.FormValue("pattern")
	if !modelPattern.MatchString(pattern) {
		http.Redirect(w, r, back+"?err=badmodel", http.StatusSeeOther)
		return
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		http.Redirect(w, r, back+"?err=notdriven", http.StatusSeeOther)
		return
	}
	if err := a.herdr.Prompt(ctx, target, "/model "+pattern); err != nil {
		a.logger.Error("model set", "err", err)
		http.Redirect(w, r, back+"?err=modelfail", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"?modelset=1", http.StatusSeeOther)
}

func (a *app) thinkingSet(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	back := "/sessions/" + url.PathEscape(key)
	level := r.FormValue("level")
	if !thinkingLevels[level] {
		http.Redirect(w, r, back+"?err=badthinking", http.StatusSeeOther)
		return
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	target, err := a.herdr.TargetForPath(ctx, meta.Path)
	if err != nil {
		http.Redirect(w, r, back+"?err=notdriven", http.StatusSeeOther)
		return
	}
	if err := a.herdr.Prompt(ctx, target, "/thinking "+level); err != nil {
		a.logger.Error("thinking set", "err", err)
		http.Redirect(w, r, back+"?err=thinkingfail", http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, back+"?thinkingset=1", http.StatusSeeOther)
}
