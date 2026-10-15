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
