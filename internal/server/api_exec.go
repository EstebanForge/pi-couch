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
	out, err := c.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		}
	}
	truncated := false
	if len(out) > execOutputCap {
		out = out[:execOutputCap]
		truncated = true
	}
	writeJSON(w, http.StatusOK, map[string]any{"output": string(out), "code": code, "truncated": truncated})
}
