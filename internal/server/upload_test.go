package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func postUpload(t *testing.T, name string, body []byte) (*httptest.ResponseRecorder, string) {
	t.Helper()
	t.Setenv("COUCHPI_UPLOAD_DIR", t.TempDir())
	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)
	fw, _ := mw.CreateFormFile("file", name)
	fw.Write(body)
	mw.Close()
	r := httptest.NewRequest("POST", "/api/upload", &buf)
	r.Header.Set("Content-Type", mw.FormDataContentType())
	w := httptest.NewRecorder()
	a := &app{logger: slog.New(slog.NewTextHandler(testDiscard{}, nil))}
	a.uploadHandler(w, r)
	var out struct {
		Path string `json:"path"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &out)
	return w, out.Path
}

func TestUploadAcceptsPNG(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, 32)...)
	w, path := postUpload(t, "shot.png", png)
	if w.Code != http.StatusOK {
		t.Fatalf("code %d body %s", w.Code, w.Body.String())
	}
	if path == "" || !strings.HasSuffix(path, ".png") {
		t.Fatalf("path %q", path)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.HasPrefix(data, png[:8]) {
		t.Fatal("content mismatch")
	}
}

func TestUploadRejectsNonImage(t *testing.T) {
	w, _ := postUpload(t, "notes.txt", []byte("just text"))
	if w.Code != http.StatusBadRequest {
		t.Fatalf("code %d", w.Code)
	}
}

func TestUploadRejectsOversize(t *testing.T) {
	png := append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}, make([]byte, maxUploadBytes+64<<10)...)
	w, _ := postUpload(t, "big.png", png)
	if w.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("code %d", w.Code)
	}
}

func TestUploadRejectsPathTraversal(t *testing.T) {
	w, path := postUpload(t, "../../evil.png", []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'})
	if w.Code != http.StatusOK {
		t.Fatalf("code %d", w.Code)
	}
	// The stored name is server-generated; the client name never reaches disk.
	if path == "" || strings.Contains(path, "..") || strings.Contains(filepath.Base(path), "evil") {
		t.Fatalf("client name leaked: %q", path)
	}
}
