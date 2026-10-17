package server

import (
	"net/http"
	"strconv"
)

// defaultPageLimit matches the reverse-pagination page the web client
// asks for when it reaches the top of the transcript.
const defaultPageLimit = 20

// apiTranscriptPage serves one page of transcript items strictly older
// than before (the newest page-1 items ascending), for the client's
// scroll-back history. more reports whether older items remain.
func (a *app) apiTranscriptPage(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	before, err := strconv.ParseInt(r.URL.Query().Get("before"), 10, 64)
	if err != nil || before < 0 {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "badrequest"})
		return
	}
	limit := defaultPageLimit
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n <= 0 || n > 100 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "badrequest"})
			return
		}
		limit = n
	}
	older, more, err := a.idx.TranscriptBefore(key, int(before), limit)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": older, "more": more})
}
