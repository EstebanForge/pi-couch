package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// The home FAB's create flow: pick a folder, write a first prompt,
// get a running pi session. cwd must live under the user's home (the
// same jail as /api/dirs) so the phone cannot spawn agents in
// arbitrary paths.

const agentCreateTimeout = 20 * time.Second

// agentName folds a folder basename into herdr's agent-name rule:
// start with a lowercase letter, only [a-z0-9-_], 1-32 chars. Real
// folders violate every one of those (leading dots, dots, upper case),
// so anything invalid becomes a letter-prefixed folded form.
func agentName(label string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(label) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteByte('-')
		}
		if b.Len() >= 30 {
			break
		}
	}
	name := strings.Trim(b.String(), "-_")
	if name == "" || name[0] < 'a' || name[0] > 'z' {
		name = "a" + name
	}
	if len(name) > 32 {
		name = name[:32]
	}
	return name
}

// waitAgentActive polls until the pane's agent is a named, promptable
// target: herdr rejects prompts with "not an active named agent" while
// the process is still launching (agent_status unknown).
func waitAgentActive(ctx context.Context, hc *herdr.Client, paneID string) error {
	deadline := time.Now().Add(15 * time.Second)
	for {
		agents, err := hc.Agents(ctx)
		if err == nil {
			for _, ag := range agents {
				if ag.PaneID == paneID && ag.AgentStatus != "" && ag.AgentStatus != "unknown" {
					return nil
				}
			}
		}
		if time.Now().After(deadline) {
			return errors.New("agent never became active")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(300 * time.Millisecond):
		}
	}
}

// apiAgents creates a herdr workspace + pi agent in cwd, submits the
// first prompt, and answers {key} once pi has a session file (it only
// appears after the first prompt, so an agent without one is invisible
// to the home list).
func (a *app) apiAgents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cwd    string `json:"cwd"`
		Prompt string `json:"prompt"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	home, err := os.UserHomeDir()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "no home directory"})
		return
	}
	cwd := filepath.Clean(req.Cwd)
	if req.Cwd == "" || !filepath.IsAbs(cwd) ||
		(cwd != home && !strings.HasPrefix(cwd, home+string(filepath.Separator))) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cwd must be a directory under home"})
		return
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "cwd must be a directory under home"})
		return
	}
	if strings.TrimSpace(req.Prompt) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), agentCreateTimeout)
	defer cancel()
	key, err := a.createAgentSession(ctx, home, cwd, req.Prompt)
	if err != nil {
		a.logger.Error("agent create failed", slog.String("cwd", cwd), slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "create failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key})
}

// createAgentSession runs the herdr sequence: workspace.create ->
// agent.start -> prompt, then waits for pi to report its session file.
// A label conflict (the folder basename already used) retries once
// with a suffix.
func (a *app) createAgentSession(ctx context.Context, home, cwd, prompt string) (string, error) {
	label := filepath.Base(cwd)
	name := agentName(label)
	workspaceID, paneID, err := a.herdr.WorkspaceCreate(ctx, label, cwd)
	if err != nil {
		label = fmt.Sprintf("%s-%04x", label, time.Now().UnixNano()&0xffff)
		name = agentName(label)
		if workspaceID, paneID, err = a.herdr.WorkspaceCreate(ctx, label, cwd); err != nil {
			return "", fmt.Errorf("workspace create: %w", err)
		}
	}
	if _, err := a.herdr.AgentStart(ctx, name, "pi", paneID); err != nil {
		_ = a.herdr.WorkspaceClose(ctx, workspaceID)
		return "", fmt.Errorf("agent start: %w", err)
	}
	if err := waitAgentActive(ctx, a.herdr, paneID); err != nil {
		_ = a.herdr.WorkspaceClose(ctx, workspaceID)
		return "", err
	}
	if err := a.herdr.Prompt(ctx, name, prompt); err != nil {
		_ = a.herdr.WorkspaceClose(ctx, workspaceID)
		return "", fmt.Errorf("prompt: %w", err)
	}

	// pi writes the session jsonl at turn start; herdr reports the path
	// on the agent once it exists. Poll rather than guessing the munged
	// sessions dir.
	deadline := time.Now().Add(agentCreateTimeout - 5*time.Second)
	for {
		agents, err := a.herdr.Agents(ctx)
		if err == nil {
			for _, ag := range agents {
				if ag.PaneID == paneID && ag.Session != nil && ag.Session.Value != "" {
					return strings.TrimSuffix(filepath.Base(ag.Session.Value), ".jsonl"), nil
				}
			}
		}
		if time.Now().After(deadline) {
			_ = a.herdr.WorkspaceClose(ctx, workspaceID)
			return "", errors.New("agent never reported a session")
		}
		select {
		case <-ctx.Done():
			_ = a.herdr.WorkspaceClose(context.Background(), workspaceID)
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
