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
	"regexp"
	"strings"
	"time"

	"github.com/EstebanForge/couch-pi/internal/herdr"
)

// The home FAB's create flow: pick a folder (or a workspace), write a
// first prompt, get a running pi session. Client-supplied cwd must
// live under the user's home (the same jail as /api/dirs) so the
// phone cannot spawn agents in arbitrary paths.

const agentCreateTimeout = 20 * time.Second

// agentNameRe bounds the pending-chat path params to the shape
// agentName() produces, so a weird name can never reach herdr.
var agentNameRe = regexp.MustCompile(`^[a-z][a-z0-9-_]{0,31}$`)

// apiAgentPrompt submits a prompt to a herdr agent by name — the
// pending chat's first message, before pi has a session file to
// target by path.
func (a *app) apiAgentPrompt(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !agentNameRe.MatchString(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad agent name"})
		return
	}
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil ||
		strings.TrimSpace(req.Text) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	if err := a.herdr.Prompt(r.Context(), name, req.Text); err != nil {
		a.logger.Error("agent prompt failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "prompt failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"ok": "true"})
}

// apiAgentSession reports the session key herdr carries for an agent,
// empty until pi writes the session file at turn start.
func (a *app) apiAgentSession(w http.ResponseWriter, r *http.Request) {
	name := r.PathValue("name")
	if !agentNameRe.MatchString(name) {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad agent name"})
		return
	}
	agents, err := a.herdr.Agents(r.Context())
	if err != nil {
		a.logger.Error("agent list failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "herdr unreachable"})
		return
	}
	key := ""
	for _, ag := range agents {
		if ag.Name == name && ag.Session != nil {
			key = strings.TrimSuffix(filepath.Base(ag.Session.Value), ".jsonl")
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key})
}

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

// apiAgents creates a pi agent and, when a prompt is supplied,
// submits it, answering {key} once pi has a session file (it only
// appears after the first prompt, so a promptless agent is invisible
// to the home list until its first turn and the key comes back
// empty). Three selectors, exactly one required: cwd (fresh workspace
// at that folder), workspace_id (split a pane in an existing
// workspace), label (reuse the labeled workspace or create it at cwd
// — the Quick flow).
func (a *app) apiAgents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cwd         string `json:"cwd"`
		WorkspaceID string `json:"workspace_id"`
		Label       string `json:"label"`
		Prompt      string `json:"prompt"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	// Exactly one selector: cwd alone (fresh workspace), workspace_id
	// alone (split), or label with its create-if-missing cwd (Quick).
	mode := ""
	switch {
	case req.WorkspaceID != "" && req.Cwd == "" && req.Label == "":
		mode = "workspace"
	case req.Label != "" && req.WorkspaceID == "" && req.Cwd != "":
		mode = "label"
	case req.Cwd != "" && req.WorkspaceID == "" && req.Label == "":
		mode = "cwd"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exactly one of cwd, workspace_id, or label with cwd"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), agentCreateTimeout)
	defer cancel()
	var key, name string
	var err error
	homeErr := false
	switch mode {
	case "workspace":
		key, name, err = a.createInWorkspace(ctx, req.WorkspaceID, req.Prompt)
	case "label":
		var cwd string
		cwd, err = a.homeDir(req.Cwd)
		homeErr = err != nil
		if err == nil {
			key, name, err = a.createInLabeledWorkspace(ctx, req.Label, cwd, req.Prompt)
		}
	default:
		var cwd string
		cwd, err = a.homeDir(req.Cwd)
		homeErr = err != nil
		if err == nil {
			key, name, err = a.createAgentSession(ctx, filepath.Base(cwd), cwd, req.Prompt)
		}
	}
	if err != nil {
		if homeErr {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
		a.logger.Error("agent create failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "create failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "agent": name})
}

// homeDir jails a client-supplied cwd under the user's home (the same
// rule as /api/dirs) and checks it is a real directory.
func (a *app) homeDir(raw string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	cwd := filepath.Clean(raw)
	if raw == "" || !filepath.IsAbs(cwd) ||
		(cwd != home && !strings.HasPrefix(cwd, home+string(filepath.Separator))) {
		return "", errors.New("cwd must be a directory under home")
	}
	if fi, err := os.Stat(cwd); err != nil || !fi.IsDir() {
		return "", errors.New("cwd must be a directory under home")
	}
	return cwd, nil
}

// apiWorkspaces lists herdr workspaces for the new-agent sheet. An
// action surface fails loudly, unlike the passive home badges.
func (a *app) apiWorkspaces(w http.ResponseWriter, r *http.Request) {
	ws, err := a.herdr.Workspaces(r.Context())
	if err != nil {
		a.logger.Error("workspace list failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "herdr unreachable"})
		return
	}
	if ws == nil {
		ws = []herdr.Workspace{}
	}
	writeJSON(w, http.StatusOK, ws)
}

// createAgentSession runs the fresh-workspace sequence:
// workspace.create -> agent.start -> prompt, then waits for pi to
// report its session file. A label conflict (the basename already
// used) retries once with a suffix.
func (a *app) createAgentSession(ctx context.Context, label, cwd, prompt string) (string, string, error) {
	name := agentName(label)
	workspaceID, paneID, err := a.herdr.WorkspaceCreate(ctx, label, cwd)
	if err != nil {
		label = fmt.Sprintf("%s-%04x", label, time.Now().UnixNano()&0xffff)
		name = agentName(label)
		if workspaceID, paneID, err = a.herdr.WorkspaceCreate(ctx, label, cwd); err != nil {
			return "", "", fmt.Errorf("workspace create: %w", err)
		}
	}
	undo := func() { _ = a.herdr.WorkspaceClose(context.Background(), workspaceID) }
	if err := a.runAgent(ctx, paneID, name, prompt, undo); err != nil {
		return "", "", err
	}
	if prompt == "" {
		return "", name, nil
	}
	key, err := a.sessionKeyForPane(ctx, paneID, undo)
	return key, name, err
}

// createInLabeledWorkspace is the Quick flow: reuse the labeled
// workspace (split a new pane for each agent) or create it at cwd on
// first use.
func (a *app) createInLabeledWorkspace(ctx context.Context, label, cwd, prompt string) (string, string, error) {
	ws, err := a.herdr.Workspaces(ctx)
	if err != nil {
		return "", "", fmt.Errorf("workspace list: %w", err)
	}
	for _, w := range ws {
		if w.Label == label {
			return a.createInWorkspace(ctx, w.WorkspaceID, prompt)
		}
	}
	return a.createAgentSession(ctx, label, cwd, prompt)
}

// createInWorkspace splits a pane in an existing workspace and starts
// the agent there; the workspace cwd applies.
func (a *app) createInWorkspace(ctx context.Context, workspaceID, prompt string) (string, string, error) {
	label := "agent"
	if ws, err := a.herdr.Workspaces(ctx); err == nil {
		for _, w := range ws {
			if w.WorkspaceID == workspaceID {
				label = w.Label
				break
			}
		}
	}
	paneID, err := a.herdr.SplitPane(ctx, workspaceID, "down")
	if err != nil {
		return "", "", fmt.Errorf("pane split: %w", err)
	}
	// Splits land in the workspace cwd, so the same folded name risks
	// colliding with the pane it split from; a suffix keeps Prompt's
	// by-name target unambiguous.
	name := agentName(label + fmt.Sprintf("-%04x", time.Now().UnixNano()&0xffff))
	undo := func() { _ = a.herdr.PaneClose(context.Background(), paneID) }
	if err := a.runAgent(ctx, paneID, name, prompt, undo); err != nil {
		return "", "", err
	}
	if prompt == "" {
		return "", name, nil
	}
	key, err := a.sessionKeyForPane(ctx, paneID, undo)
	return key, name, err
}

// runAgent starts pi in the pane and submits the first prompt; undo
// tears the pane (or fresh workspace) down on any failure. agent.start
// retries while herdr reports the pane shell not yet available — a
// fresh workspace's root shell takes a moment to spawn, and the bind
// is otherwise a race.
func (a *app) runAgent(ctx context.Context, paneID, name, prompt string, undo func()) error {
	var err error
	for i := 0; i < 5; i++ {
		if _, err = a.herdr.AgentStart(ctx, name, "pi", paneID); err == nil ||
			!strings.Contains(err.Error(), "not an available shell") {
			break
		}
		select {
		case <-ctx.Done():
			undo()
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
	if err != nil {
		undo()
		return fmt.Errorf("agent start: %w", err)
	}
	if err := waitAgentActive(ctx, a.herdr, paneID); err != nil {
		undo()
		return err
	}
	if prompt == "" {
		return nil
	}
	if err := a.herdr.Prompt(ctx, name, prompt); err != nil {
		undo()
		return fmt.Errorf("prompt: %w", err)
	}
	return nil
}

// sessionKeyForPane waits for pi to report its session file: pi writes
// the session jsonl at turn start; herdr carries the path on the
// agent once it exists. Poll rather than guessing the munged sessions
// dir.
func (a *app) sessionKeyForPane(ctx context.Context, paneID string, undo func()) (string, error) {
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
			undo()
			return "", errors.New("agent never reported a session")
		}
		select {
		case <-ctx.Done():
			undo()
			return "", ctx.Err()
		case <-time.After(500 * time.Millisecond):
		}
	}
}
