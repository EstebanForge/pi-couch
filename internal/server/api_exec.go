package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Quick actions can carry plain bash entries for one-off tasks; they
// run on the box in the session's folder. Same trust level as every
// other surface: the owner's tunnel is the boundary.

// Package var so tests can shrink the window.
var execTimeout = 60 * time.Second

const execOutputCap = 64 << 10

func (a *app) apiExec(w http.ResponseWriter, r *http.Request) {
	if !a.execTokenGate(w, r) {
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var req struct {
		Cmd string `json:"cmd"`
		Cwd string `json:"cwd"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	cmd := strings.TrimSpace(req.Cmd)
	if cmd == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	home := a.home
	if home == "" {
		home, _ = os.UserHomeDir()
	}
	dir := req.Cwd
	if dir == "" {
		dir = home
	}
	if home == "" || !(dir == home || strings.HasPrefix(dir, home+string(filepath.Separator))) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cwd must be under home"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), execTimeout)
	defer cancel()
	c := exec.CommandContext(ctx, "bash", "-c", cmd)
	c.Dir = dir
	// A backgrounded child inherits the pipes: without WaitDelay, Wait
	// blocks until the child exits, long past the command timeout.
	c.WaitDelay = 2 * time.Second
	// Cap the capture while the command runs, not after: a firehose
	// would buffer unbounded memory before any truncation. Past the cap
	// output is drained and discarded so the pipe never blocks.
	buf := &boundedBuffer{max: execOutputCap}
	c.Stdout = buf
	c.Stderr = buf
	runErr := c.Run()
	code := 0
	if runErr != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(runErr, &ee) {
			code = ee.ExitCode()
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": string(buf.buf), "code": code, "truncated": buf.full})
}

// Exec is a shell: it stays token-gated even when no token is
// configured, or every local Unix user could drive it on the loopback
// bind.
func (a *app) execTokenGate(w http.ResponseWriter, r *http.Request) bool {
	if a.tokenOK(r) {
		return true
	}
	writeJSON(w, http.StatusForbidden, map[string]string{"error": "exec requires an access token"})
	return false
}

type boundedBuffer struct {
	buf  []byte
	max  int
	full bool
}

func (b *boundedBuffer) Write(p []byte) (int, error) {
	if b.full {
		return len(p), nil
	}
	if room := b.max - len(b.buf); len(p) > room {
		b.buf = append(b.buf, p[:room]...)
		b.full = true
		return len(p), nil
	}
	b.buf = append(b.buf, p...)
	return len(p), nil
}
