package server

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// Prompt image attachments (D6). herdr prompts are text-only, but pi picks
// up an image when its path appears in the prompt text (the herdr desk
// clipboard works the same way: a file under /tmp and the path in the
// message). Upload stores the bytes, the composer appends the returned
// path to the message.

const maxUploadBytes = 10 << 20

// uploadDir is /tmp so herdr's pi (same machine, no private-tmp in the
// unit) can read it; the OS clears it like herdr's own clipboard dir.
const uploadDir = "/tmp/couch-pi-uploads"

var sniffExts = map[string]string{
	"image/png":  ".png",
	"image/jpeg": ".jpg",
	"image/webp": ".webp",
	"image/gif":  ".gif",
}

// sanitizeExt folds the client extension to a safe form. It is the only
// piece of the client name that touches disk, and only after this gate.
func sanitizeExt(clientName, ctype string) string {
	e := strings.ToLower(filepath.Ext(clientName))
	ok := len(e) >= 2 && len(e) <= 9 && e[0] == '.'
	for i := 1; ok && i < len(e); i++ {
		c := e[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9') {
			ok = false
		}
	}
	if ok {
		return e
	}
	return sniffExts[ctype]
}

func (a *app) uploadHandler(w http.ResponseWriter, r *http.Request) {
	dir := os.Getenv("COUCHPI_UPLOAD_DIR")
	if dir == "" {
		dir = uploadDir
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		writeJSON(w, http.StatusRequestEntityTooLarge, map[string]string{"error": "too large (10MB cap)"})
		return
	}
	f, header, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing file"})
		return
	}
	defer f.Close()
	head := make([]byte, 512)
	n, _ := f.Read(head)
	ctype := http.DetectContentType(head[:n])
	if _, err := f.Seek(0, 0); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "rewind failed"})
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		a.logger.Error("upload mkdir", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	var suffix [4]byte
	if _, err := rand.Read(suffix[:]); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	// The name is server-generated; the client name never touches disk
	// beyond its sanitized extension.
	ext := sanitizeExt(header.Filename, ctype)
	name := "couchpi-" + time.Now().UTC().Format("20060102-150405") + "-" + hex.EncodeToString(suffix[:]) + ext
	dst, err := os.Create(filepath.Join(dir, name))
	if err != nil {
		a.logger.Error("upload create", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	defer dst.Close()
	// head was only for the content sniff; the rewound file carries the
	// full body, so writing head first would duplicate its prefix.
	if _, err := dst.ReadFrom(f); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "store failed"})
		return
	}
	_ = dst.Chmod(0o644)
	writeJSON(w, http.StatusOK, map[string]string{"path": filepath.Join(dir, name)})
}
