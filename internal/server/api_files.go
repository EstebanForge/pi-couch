package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"path"
	"time"

	"github.com/EstebanForge/couch-pi/internal/browse"
	"github.com/EstebanForge/couch-pi/internal/git"
	"github.com/EstebanForge/couch-pi/internal/store"
)

// changeFile is the wire shape of one git.FileChange row.
type changeFile struct {
	Path    string `json:"path"`
	Staged  string `json:"staged"`
	Work    string `json:"work"`
	Added   int    `json:"added"`
	Deleted int    `json:"deleted"`
	Binary  bool   `json:"binary"`
	Label   string `json:"label"`
}

func wireFiles(files []git.FileChange) []changeFile {
	out := make([]changeFile, len(files))
	for i, f := range files {
		out[i] = changeFile{
			Path: f.Path, Staged: f.Staged, Work: f.Work,
			Added: f.Added, Deleted: f.Deleted, Binary: f.Binary,
			Label: f.Label(),
		}
	}
	return out
}

// apiChanges is the JSON twin of the templ changes page: no file query
// lists the working tree, ?file= returns that file's diff text.
func (a *app) apiChanges(w http.ResponseWriter, r *http.Request) {
	meta, ok := a.idx.Meta(r.PathValue("key"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	repo, err := git.Toplevel(ctx, meta.Cwd)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"repo": false, "branch": "", "files": []changeFile{}})
		return
	}
	branch := git.Branch(ctx, repo)
	if sel := r.URL.Query().Get("file"); sel != "" {
		files, err := git.Status(ctx, repo)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "gitfail"})
			return
		}
		known := false
		stagedOnly := false
		for _, f := range files {
			if f.Path == sel {
				known = f.Label() != "new" && !f.Binary
				stagedOnly = f.Work == " " || f.Work == ""
				break
			}
		}
		if !known {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
			return
		}
		diff, truncated, err := git.Diff(ctx, repo, sel, stagedOnly)
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "gitfail"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"file": sel, "diff": diff, "truncated": truncated})
		return
	}
	files, err := git.Status(ctx, repo)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "gitfail"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo": true, "branch": branch, "files": wireFiles(files)})
}

// apiStreamChanges pushes hash-gated reset events with the file list
// on the same 3 s poll as the datastar stream, plus pings. The client
// refetches its open diff when the list moves.
func (a *app) apiStreamChanges(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	meta, ok := a.idx.Meta(key)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	lastHash := ""
	lastSend := time.Now()
	for {
		ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
		var files []git.FileChange
		if repo, err := git.Toplevel(ctx, meta.Cwd); err == nil {
			if fs, err := git.Status(ctx, repo); err == nil {
				files = fs
			}
		}
		cancel()
		sum := sha256.Sum256([]byte(fmt.Sprintf("%v", files)))
		hash := hex.EncodeToString(sum[:8])
		if hash != lastHash {
			lastHash = hash
			lastSend = time.Now()
			payload, _ := json.Marshal(map[string]any{"files": wireFiles(files)})
			if !writeSSE(w, "reset", string(payload)) {
				return
			}
		} else if time.Since(lastSend) >= keepaliveEvery {
			lastSend = time.Now()
			if !writeSSE(w, "ping", "{}") {
				return
			}
		}
		select {
		case <-time.After(changesPoll):
		case <-r.Context().Done():
			return
		}
	}
}

// browseEntry is the wire shape of one browse.Entry row.
type browseEntry struct {
	Name string `json:"name"`
	Dir  bool   `json:"dir"`
	Size int64  `json:"size"`
}

// apiFiles lists one directory of the session's repo.
func (a *app) apiFiles(w http.ResponseWriter, r *http.Request) {
	meta, ok := a.idx.Meta(r.PathValue("key"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	repo, err := git.Toplevel(ctx, meta.Cwd)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"repo": false, "dir": "", "entries": []browseEntry{}})
		return
	}
	rel := path.Clean(r.URL.Query().Get("dir"))
	if rel == "." || rel == "/" {
		rel = ""
	}
	entries, err := browse.List(repo, rel)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "baddir"})
		return
	}
	out := make([]browseEntry, len(entries))
	for i, e := range entries {
		out[i] = browseEntry{Name: e.Name, Dir: e.Dir, Size: e.Size}
	}
	writeJSON(w, http.StatusOK, map[string]any{"repo": true, "dir": rel, "entries": out})
}

// apiFilesView previews one repo file as {file, kind, content}.
func (a *app) apiFilesView(w http.ResponseWriter, r *http.Request) {
	meta, ok := a.idx.Meta(r.PathValue("key"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	repo, err := git.Toplevel(ctx, meta.Cwd)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "notrepo"})
		return
	}
	file := path.Clean(r.URL.Query().Get("file"))
	content, kind, err := browse.Preview(repo, file)
	if err != nil {
		if errors.Is(err, browse.ErrEscape) {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "escape"})
			return
		}
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "badfile"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"file": file, "kind": kind, "content": content})
}

// apiComments returns the session's sent-comment history.
func (a *app) apiComments(w http.ResponseWriter, r *http.Request) {
	meta, ok := a.idx.Meta(r.PathValue("key"))
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	sent, err := a.st.Sent(meta.Key)
	if err != nil {
		// History is additive UI; a broken state file must not 500.
		slog.Error("sent comments", "err", err)
		sent = nil
	}
	if sent == nil {
		sent = []store.Comment{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"sent": sent})
}
