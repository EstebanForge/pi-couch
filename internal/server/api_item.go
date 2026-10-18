package server

import (
	"net/http"
	"strconv"
)

// apiItem serves one transcript item with no render truncation, so the
// phone can expand tool output and bash text on demand.
func (a *app) apiItem(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	_, it, err := a.idx.FullItem(key, seq)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"text": it.Text})
}

// apiItemImage serves the decoded bytes of an item's first image block
// (user attachment or read-tool result). Plain 404s, not JSON: the
// <img> tag is the only consumer. Session entries are append-only, so
// the response caches forever.
func (a *app) apiItemImage(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	seq, err := strconv.Atoi(r.PathValue("seq"))
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	data, mime, err := a.idx.ItemImage(key, seq)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", mime)
	w.Header().Set("Cache-Control", "private, max-age=31536000, immutable")
	w.Header().Set("Content-Length", strconv.Itoa(len(data)))
	w.Write(data)
}
