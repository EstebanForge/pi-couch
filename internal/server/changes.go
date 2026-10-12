package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/starfederation/datastar-go/datastar"

	"github.com/EstebanForge/couch-pi/internal/git"
	"github.com/EstebanForge/couch-pi/internal/web/pages"
)

func (a *app) changes(w http.ResponseWriter, r *http.Request) {
	meta, ok := a.idx.Meta(r.PathValue("key"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	repo, err := git.Toplevel(ctx, meta.Cwd)
	if err != nil {
		serveTempl(w, r, pages.Changes(meta, "", "", nil, "", "", false))
		return
	}
	branch := git.Branch(ctx, repo)
	selFile := r.URL.Query().Get("file")
	var files []git.FileChange
	var diffText string
	var truncated bool
	if selFile != "" {
		// Only paths git itself reports may be diffed.
		files, err = git.Status(ctx, repo)
		if err != nil {
			http.Error(w, "git status failed", http.StatusInternalServerError)
			return
		}
		known := false
		stagedOnly := false
		for _, f := range files {
			if f.Path == selFile && f.Label() != "new" && !f.Binary {
				known = true
				// A staged-only change has no worktree side, so only the
				// cached diff has content.
				stagedOnly = f.Work == " " || f.Work == ""
				break
			}
		}
		if !known {
			http.Redirect(w, r, "/sessions/"+url.PathEscape(meta.Key)+"/changes", http.StatusSeeOther)
			return
		}
		diffText, truncated, err = git.Diff(ctx, repo, selFile, stagedOnly)
		if err != nil {
			http.Error(w, "git diff failed", http.StatusInternalServerError)
			return
		}
	} else {
		files, err = git.Status(ctx, repo)
		if err != nil {
			http.Error(w, "git status failed", http.StatusInternalServerError)
			return
		}
	}
	serveTempl(w, r, pages.Changes(meta, repo, branch, files, selFile, diffText, truncated))
}

// streamChanges patches the file list on a 3 s poll, hash-gated so idle
// trees emit nothing (settled R4 policy: throttle + no-send when equal).
func (a *app) streamChanges(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	meta, ok := a.idx.Meta(key)
	if !ok {
		http.NotFound(w, r)
		return
	}
	sse := datastar.NewSSE(w, r)
	lastHash := ""
	for {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		repo, err := git.Toplevel(ctx, meta.Cwd)
		if err == nil {
			files, err := git.Status(ctx, repo)
			if err == nil {
				sum := sha256.Sum256([]byte(fmt.Sprintf("%v", files)))
				hash := hex.EncodeToString(sum[:8])
				if hash != lastHash {
					lastHash = hash
					if err := sse.PatchElementTempl(pages.FileCards(key, files), datastar.WithSelector("#file-list"), datastar.WithModeInner()); err != nil {
						cancel()
						return
					}
				}
			}
		}
		cancel()
		select {
		case <-time.After(changesPoll):
		case <-r.Context().Done():
			return
		}
	}
}
