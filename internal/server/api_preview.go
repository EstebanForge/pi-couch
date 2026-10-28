package server

import (
	"net/http"
	"strings"
)

// previewConvLines is how many user/assistant texts the preview keeps:
// a glance, not a transcript.
const previewConvLines = 6

// previewTextCap matches the render path's per-item budget. The index
// caps only tool text, so the preview caps conversation text itself;
// newlines flatten because a preview line is one row on a phone card.
const previewTextCap = 400

func previewText(s string) string {
	s = strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	r := []rune(s)
	if len(r) <= previewTextCap {
		return s
	}
	return string(r[:previewTextCap-1]) + "…"
}

type previewLine struct {
	Seq  int    `json:"seq"`
	Who  string `json:"who"`
	Text string `json:"text"`
}

// apiSessionPreview serves the home card's hold-to-preview: the last
// few user/assistant texts of the active branch (text already capped
// by the render limit), plus the same status vocabulary the card
// shows - herdr badges for the dot, the awaiting flag for the yellow
// spinner. One frozen snapshot per fetch; closed sessions answer from
// their files like the transcript does.
func (a *app) apiSessionPreview(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	meta, items, err := a.idx.Transcript(key, 0)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	lines := make([]previewLine, 0, previewConvLines)
	for i := len(items) - 1; i >= 0 && len(lines) < previewConvLines; i-- {
		switch items[i].Kind {
		case "user":
			lines = append(lines, previewLine{Seq: items[i].Seq, Who: "user", Text: previewText(items[i].Text)})
		case "text":
			lines = append(lines, previewLine{Seq: items[i].Seq, Who: "assistant", Text: previewText(items[i].Text)})
		}
	}
	for i, j := 0, len(lines)-1; i < j; i, j = i+1, j-1 {
		lines[i], lines[j] = lines[j], lines[i]
	}
	badges, _ := a.herdr.DriveStatus(r.Context())
	status := badges[meta.Path]
	if status == "" {
		status = "unknown"
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"title":    meta.DisplayName(),
		"status":   status,
		"awaiting": a.awaiting.snapshot()[meta.Path],
		"lines":    lines,
	})
}
