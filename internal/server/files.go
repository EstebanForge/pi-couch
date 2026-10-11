package server

import (
	"context"
	"net/http"
	"path"
	"time"

	"github.com/EstebanForge/couch-pi/internal/browse"
	"github.com/EstebanForge/couch-pi/internal/git"
	"github.com/EstebanForge/couch-pi/internal/web/pages"
)

// files lists one directory of the session's repo (lazy tree by
// navigation); files/view previews a file. Both are read-only.
func (a *app) files(w http.ResponseWriter, r *http.Request) {
	meta, ok := a.idx.Meta(r.PathValue("key"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	repo, err := git.Toplevel(ctx, meta.Cwd)
	if err != nil {
		serveTempl(w, r, pages.Files(meta, "", "", nil))
		return
	}
	rel := path.Clean(r.URL.Query().Get("dir"))
	if rel == "." || rel == "/" {
		rel = ""
	}
	entries, err := browse.List(repo, rel)
	if err != nil {
		http.Error(w, "cannot list directory", http.StatusBadRequest)
		return
	}
	serveTempl(w, r, pages.Files(meta, repo, rel, entries))
}

func (a *app) filesView(w http.ResponseWriter, r *http.Request) {
	meta, ok := a.idx.Meta(r.PathValue("key"))
	if !ok {
		http.NotFound(w, r)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	repo, err := git.Toplevel(ctx, meta.Cwd)
	if err != nil {
		http.Error(w, "not a repository", http.StatusBadRequest)
		return
	}
	file := path.Clean(r.URL.Query().Get("file"))
	content, kind, err := browse.Preview(repo, file)
	if err != nil {
		http.Error(w, "cannot read file", http.StatusBadRequest)
		return
	}
	serveTempl(w, r, pages.PreviewPage(meta, repo, file, content, kind))
}
