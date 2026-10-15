package server

import (
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Directory listing for the new-agent folder picker. The jail is the
// user's home: only paths under it are listed, and symlinked dirs
// whose real target escapes home are skipped, so the phone can browse
// nothing else (the plan's allowlist + symlink rule).

const dirsCap = 500

type dirEntry struct {
	Name string `json:"name"`
	Path string `json:"path"`
}

type dirsResponse struct {
	Path      string     `json:"path"`
	Parent    string     `json:"parent,omitempty"`
	Dirs      []dirEntry `json:"dirs"`
	Truncated bool       `json:"truncated,omitempty"`
	Error     string     `json:"error,omitempty"`
}

// listDirs clamps target into home and lists its subdirectories. A
// relative or outside path falls back to home rather than erroring,
// so the picker always shows something sane.
func listDirs(home, target string) dirsResponse {
	if home == "" {
		return dirsResponse{Error: "no home directory"}
	}
	path := filepath.Clean(target)
	if !filepath.IsAbs(path) || (path != home && !strings.HasPrefix(path, home+string(filepath.Separator))) {
		path = home
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		return dirsResponse{Path: home, Dirs: []dirEntry{}, Error: "cannot read directory"}
	}
	out := []dirEntry{}
	truncated := false
	for _, e := range entries {
		if e.Type()&os.ModeDir == 0 {
			continue
		}
		full := filepath.Join(path, e.Name())
		// A symlinked dir whose resolved target leaves home is invisible.
		if resolved, err := filepath.EvalSymlinks(full); err != nil ||
			(resolved != home && !strings.HasPrefix(resolved, home+string(filepath.Separator))) {
			continue
		}
		if len(out) >= dirsCap {
			truncated = true
			break
		}
		out = append(out, dirEntry{Name: e.Name(), Path: full})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	resp := dirsResponse{Path: path, Dirs: out, Truncated: truncated}
	if path != home {
		resp.Parent = filepath.Dir(path)
	}
	return resp
}

// apiDirs answers GET /api/dirs?path=...; the guard's /api host gate
// covers it like every other JSON read.
func (a *app) apiDirs(w http.ResponseWriter, r *http.Request) {
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no home directory"})
		return
	}
	writeJSON(w, http.StatusOK, listDirs(home, r.URL.Query().Get("path")))
}
