package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func postExec(t *testing.T, a *app, h http.Handler, body string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/api/exec", strings.NewReader(body))
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestExecRunsSequences(t *testing.T) {
	home := t.TempDir()
	a, _, h := streamTestApp(t)
	a.home = home

	rec := postExec(t, a, h, `{"cmd":"echo hi && pwd","cwd":"`+home+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Output string `json:"output"`
		Code   int    `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code != 0 {
		t.Fatalf("code %d output %q", out.Code, out.Output)
	}
	if !strings.Contains(out.Output, "hi") || !strings.Contains(out.Output, home) {
		t.Fatalf("output %q", out.Output)
	}
}

func TestExecRejectsOutsideHome(t *testing.T) {
	a, _, h := streamTestApp(t)
	rec := postExec(t, a, h, `{"cmd":"echo hi","cwd":"/etc"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
}

func TestExecTimesOut(t *testing.T) {
	old := execTimeout
	execTimeout = 150 * time.Millisecond
	defer func() { execTimeout = old }()

	home := t.TempDir()
	a, _, h := streamTestApp(t)
	a.home = home

	rec := postExec(t, a, h, `{"cmd":"sleep 5","cwd":"`+home+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Code int `json:"code"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out.Code == 0 {
		t.Fatalf("a timed-out command must not report success")
	}
}

func TestExecEmptyCmd(t *testing.T) {
	a, _, h := streamTestApp(t)
	rec := postExec(t, a, h, `{"cmd":"  ","cwd":"`+a.home+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.home, "sentinel")); err == nil {
		t.Fatal("sentinel must stay absent")
	}
}
