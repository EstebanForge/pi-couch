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
	if a.accessToken != "" {
		req.Header.Set("Authorization", "Bearer "+a.accessToken)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// execApp hands back a tokened app: exec refuses tokenless apps.
func execApp(t *testing.T) (*app, http.Handler, string) {
	a, _, h := streamTestApp(t)
	const tok = "tok"
	a.accessToken = tok
	return a, h, tok
}

func TestExecRunsSequences(t *testing.T) {
	home := t.TempDir()
	a, _, h := streamTestApp(t)
	a.home = home
	a.accessToken = "tok"

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
	a.accessToken = "tok"
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
	a.accessToken = "tok"

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
	a.accessToken = "tok"
	rec := postExec(t, a, h, `{"cmd":"  ","cwd":"`+a.home+`"}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	if _, err := os.Stat(filepath.Join(a.home, "sentinel")); err == nil {
		t.Fatal("sentinel must stay absent")
	}
}

// A backgrounded child inherits the output pipes; without WaitDelay
// Wait blocks until the child exits, long past the command timeout.
func TestExecOutlivesBackgroundChild(t *testing.T) {
	old := execTimeout
	execTimeout = 250 * time.Millisecond
	defer func() { execTimeout = old }()

	home := t.TempDir()
	a, _, h := streamTestApp(t)
	a.home = home
	a.accessToken = "tok"

	done := make(chan *httptest.ResponseRecorder, 1)
	go func() {
		done <- postExec(t, a, h, `{"cmd":"sleep 30 & echo bg; wait","cwd":"`+home+`"}`)
	}()
	select {
	case rec := <-done:
		if rec.Code != http.StatusOK {
			t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
		}
		if !strings.Contains(rec.Body.String(), "bg") {
			t.Fatalf("output %q", rec.Body.String())
		}
	case <-time.After(5 * time.Second):
		t.Fatal("handler hung on a backgrounded child")
	}
}

// Output is bounded while the command runs, not after: a yes-firehose
// must not buffer gigabytes before the handler truncates.
func TestExecBoundsOutputWhileRunning(t *testing.T) {
	home := t.TempDir()
	a, _, h := streamTestApp(t)
	a.home = home
	a.accessToken = "tok"

	rec := postExec(t, a, h, `{"cmd":"yes | head -c 20000000","cwd":"`+home+`"}`)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	var out struct {
		Output    string `json:"output"`
		Truncated bool   `json:"truncated"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if !out.Truncated || len(out.Output) != execOutputCap {
		t.Fatalf("truncated=%v len=%d", out.Truncated, len(out.Output))
	}
}

// Exec is a shell: it stays token-gated even when no token is
// configured, or every local Unix user could drive it on the loopback
// bind.
func TestExecRequiresToken(t *testing.T) {
	home := t.TempDir()
	a, _, h := streamTestApp(t)
	a.home = home

	rec := postExec(t, a, h, `{"cmd":"echo hi","cwd":"`+home+`"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("tokenless exec: code %d body %s", rec.Code, rec.Body.String())
	}

	a.accessToken = "sekrit"
	req := httptest.NewRequest(http.MethodPost, "/api/exec", strings.NewReader(`{"cmd":"echo hi","cwd":"`+home+`"}`))
	req.Host = "127.0.0.1:8090"
	req.Header.Set("Authorization", "Bearer sekrit")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req)
	if rec2.Code != http.StatusOK || !strings.Contains(rec2.Body.String(), "hi") {
		t.Fatalf("tokened exec: code %d body %s", rec2.Code, rec2.Body.String())
	}
}
