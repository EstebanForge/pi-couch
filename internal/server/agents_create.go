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

// herdrRetry rides out herdr's transient socket stalls: under load
// the unix socket intermittently answers dial timeouts while the
// process is fine moments later, and one timeout must not abort a
// create. Only wire-safe for calls that are reads or whose failure
// means herdr never received the request.
func herdrRetry[T any](f func() (T, error)) (T, error) {
	var zero T
	var err error
	for i := 0; i < 8; i++ {
		var v T
		start := time.Now()
		v, err = f()
		if err == nil || !strings.Contains(err.Error(), "herdr: dial:") {
			return v, err
		}
		slog.Warn("herdr dial stalled", slog.Int("attempt", i+1), slog.Float64("elapsed_s", time.Since(start).Seconds()), slog.String("error", err.Error()))
		time.Sleep(1200 * time.Millisecond)
	}
	return zero, err
}

// herdrRetryErr is herdrRetry for error-only calls.
func herdrRetryErr(f func() error) error {
	_, err := herdrRetry(func() (struct{}, error) { return struct{}{}, f() })
	return err
}

// paneReadyWait bounds the wait for pi's TUI to start reading input.
var paneReadyWait = 20 * time.Second

// piReadyRe matches the context%/window stats footer pi paints once
// its TUI reads input (e.g. "0.0%/1.0M").
var piReadyRe = regexp.MustCompile(`%/\d`)

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

// apiAgents creates a pi agent and submits the first prompt,
// answering {key} once pi has a session file (it only appears after
// the first prompt). Three selectors, exactly one required: cwd
// (fresh workspace at that folder), workspace_id (split a pane in an
// existing workspace), label (reuse the labeled workspace or create
// it at cwd — the Quick flow).
func (a *app) apiAgents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cwd         string `json:"cwd"`
		WorkspaceID string `json:"workspace_id"`
		Label       string `json:"label"`
		Prompt      string `json:"prompt"`
		Model       string `json:"model"`
		Thinking    string `json:"thinking"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	req.Prompt = strings.TrimSpace(req.Prompt)
	if req.Prompt == "" {
		// pi writes the session file only at turn start, so a
		// promptless agent would never surface on the home list.
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "prompt is required"})
		return
	}
	piArgs, err := a.startArgs(req.Model, req.Thinking)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
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
	homeErr := false
	if a.hcDrv != nil {
		// hc has no workspaces: the Quick flow's label folds into a
		// plain spawn at the folder, and workspace splits cannot exist.
		if mode == "workspace" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "workspaces need the herdr driver"})
			return
		}
		var cwd string
		cwd, err = a.homeDir(req.Cwd)
		homeErr = err != nil
		if err == nil {
			key, name, err = a.createAgentSessionHC(ctx, filepath.Base(cwd), cwd, req.Prompt, piArgs)
		}
	} else {
		switch mode {
		case "workspace":
			key, name, err = a.createInWorkspace(ctx, req.WorkspaceID, req.Prompt, piArgs)
		case "label":
			var cwd string
			cwd, err = a.homeDir(req.Cwd)
			homeErr = err != nil
			if err == nil {
				key, name, err = a.createInLabeledWorkspace(ctx, req.Label, cwd, req.Prompt, piArgs)
			}
		default:
			var cwd string
			cwd, err = a.homeDir(req.Cwd)
			homeErr = err != nil
			if err == nil {
				key, name, err = a.createAgentSession(ctx, filepath.Base(cwd), cwd, req.Prompt, piArgs)
			}
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

// startArgs turns the create panel's model/thinking picks into pi
// command-line flags. A bad pi flag exits pi at launch and the create
// would time out waiting for an agent that never binds, so both are
// refused here: the model must exist in the catalog and the thinking
// level must be one pi knows. Thinking rides the --model value
// (pi's "id:level" suffix), so it needs a model.
func (a *app) startArgs(model, thinking string) ([]string, error) {
	if model == "" && thinking == "" {
		return nil, nil
	}
	if model == "" {
		return nil, errors.New("thinking requires a model")
	}
	switch thinking {
	case "", "off", "minimal", "low", "medium", "high", "xhigh", "max":
	default:
		return nil, errors.New("bad thinking level")
	}
	rows, err := a.models.load(context.Background())
	if err == nil {
		found := false
		for _, row := range rows {
			if row.ID == model {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("unknown model")
		}
	}
	// A catalog failure passes through: pi reports the bad model and
	// the create fails loudly instead of silently dropping the pick.
	if thinking != "" {
		return []string{"--model", model + ":" + thinking}, nil
	}
	return []string{"--model", model}, nil
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
// action surface fails loudly, unlike the passive home badges. Under
// hc there are no workspaces, so the sheet sees an empty list.
func (a *app) apiWorkspaces(w http.ResponseWriter, r *http.Request) {
	if a.hcDrv != nil {
		writeJSON(w, http.StatusOK, []herdr.Workspace{})
		return
	}
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
func (a *app) createAgentSession(ctx context.Context, label, cwd, prompt string, piArgs []string) (string, string, error) {
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
	if err := a.runAgent(ctx, paneID, name, prompt, piArgs, undo); err != nil {
		return "", "", err
	}
	key, err := a.sessionKeyForPane(ctx, paneID, undo)
	return key, name, err
}

// createInLabeledWorkspace is the Quick flow: reuse the labeled
// workspace (split a new pane for each agent) or create it at cwd on
// first use.
func (a *app) createInLabeledWorkspace(ctx context.Context, label, cwd, prompt string, piArgs []string) (string, string, error) {
	ws, err := a.herdr.Workspaces(ctx)
	if err != nil {
		return "", "", fmt.Errorf("workspace list: %w", err)
	}
	for _, w := range ws {
		if w.Label == label {
			return a.createInWorkspace(ctx, w.WorkspaceID, prompt, piArgs)
		}
	}
	return a.createAgentSession(ctx, label, cwd, prompt, piArgs)
}

// createInWorkspace splits a pane in an existing workspace and starts
// the agent there; the workspace cwd applies.
func (a *app) createInWorkspace(ctx context.Context, workspaceID, prompt string, piArgs []string) (string, string, error) {
	label := "agent"
	if ws, err := a.herdr.Workspaces(ctx); err == nil {
		for _, w := range ws {
			if w.WorkspaceID == workspaceID {
				label = w.Label
				break
			}
		}
	}
	paneID, err := herdrRetry(func() (string, error) { return a.herdr.SplitPane(ctx, workspaceID, "down") })
	if err != nil {
		return "", "", fmt.Errorf("pane split: %w", err)
	}
	// Splits land in the workspace cwd, so the same folded name risks
	// colliding with the pane it split from; a suffix keeps Prompt's
	// by-name target unambiguous.
	name := agentName(label + fmt.Sprintf("-%04x", time.Now().UnixNano()&0xffff))
	undo := func() { _ = a.herdr.PaneClose(context.Background(), paneID) }
	if err := a.runAgent(ctx, paneID, name, prompt, piArgs, undo); err != nil {
		return "", "", err
	}
	key, err := a.sessionKeyForPane(ctx, paneID, undo)
	return key, name, err
}

// resumeSession starts pi again over a closed session's jsonl via
// --session, which loads that exact file and keeps appending to it,
// so the phone's session key never changes. text (may be empty) is
// submitted as the first prompt once the TUI is ready.
func (a *app) resumeSession(ctx context.Context, label, cwd, path, text string) (string, error) {
	name := agentName(label)
	workspaceID, paneID, err := a.herdr.WorkspaceCreate(ctx, label, cwd)
	if err != nil {
		label = fmt.Sprintf("%s-%04x", label, time.Now().UnixNano()&0xffff)
		name = agentName(label)
		if workspaceID, paneID, err = a.herdr.WorkspaceCreate(ctx, label, cwd); err != nil {
			return "", fmt.Errorf("workspace create: %w", err)
		}
	}
	undo := func() { _ = a.herdr.WorkspaceClose(context.Background(), workspaceID) }
	if err := a.runAgent(ctx, paneID, name, text, []string{"--session", path}, undo); err != nil {
		return "", err
	}
	return name, nil
}

// apiSessionResume restarts a closed session: a fresh workspace at
// the session's cwd runs pi --session <jsonl>, and the optional text
// rides along as the first prompt. The jsonl already exists, so the
// answer needs no session discovery — the requested key is the key.
func (a *app) apiSessionResume(w http.ResponseWriter, r *http.Request) {
	key := r.PathValue("key")
	var req struct {
		Text string `json:"text"`
	}
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "bad request"})
		return
	}
	meta, ok := a.idx.Meta(key)
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "notfound"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), agentCreateTimeout)
	defer cancel()
	if _, err := a.statusFor().TargetForPath(ctx, meta.Path); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "running"})
		return
	}
	cwd, err := a.homeDir(meta.Cwd)
	if err != nil {
		// The session's project may be gone; pi --session takes an
		// absolute path, so the workspace cwd only needs to be legal.
		cwd, err = os.UserHomeDir()
		if err != nil {
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "resume failed"})
			return
		}
	}
	label := meta.Name
	if label == "" {
		label = filepath.Base(cwd)
	}
	if a.hcDrv != nil {
		name, err := a.resumeSessionHC(ctx, label, cwd, meta.Path, strings.TrimSpace(req.Text))
		if err != nil {
			a.logger.Error("resume failed", slog.String("key", key), slog.String("error", err.Error()))
			writeJSON(w, http.StatusBadGateway, map[string]string{"error": "resume failed"})
			return
		}
		writeJSON(w, http.StatusOK, map[string]string{"key": key, "agent": name})
		return
	}
	name, err := a.resumeSession(ctx, label, cwd, meta.Path, strings.TrimSpace(req.Text))
	if err != nil {
		a.logger.Error("resume failed", slog.String("key", key), slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "resume failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "agent": name})
}

// runAgent starts pi in the pane and submits the first prompt; undo
// tears the pane (or fresh workspace) down on any failure. agent.start
// retries while herdr reports the pane shell not yet available — a
// fresh workspace's root shell takes a moment to spawn, and the bind
// is otherwise a race.
func (a *app) runAgent(ctx context.Context, paneID, name, prompt string, piArgs []string, undo func()) error {
	var err error
	for i := 0; i < 5; i++ {
		if _, err = herdrRetry(func() (herdr.Agent, error) {
			return a.herdr.AgentStart(ctx, name, "pi", paneID, piArgs)
		}); err == nil ||
			!strings.Contains(err.Error(), "not an available shell") && !strings.Contains(err.Error(), "herdr: dial:") {
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
	// pi's TUI needs seconds of node startup before it reads input;
	// a prompt delivered earlier lands in the shell's stdin and is
	// lost, and the create then never starts a turn. The ready footer
	// (context%/window stats line) is the signal that the TUI listens.

	start := time.Now()
	if !a.waitPaneReady(ctx, paneID, paneReadyWait) {
		a.logger.Warn("pane never ready", slog.Float64("waited_s", time.Since(start).Seconds()))
		undo()
		return errors.New("pi never became ready")
	}
	a.logger.Info("pane ready", slog.Float64("waited_s", time.Since(start).Seconds()))
	if prompt != "" {
		if err := herdrRetryErr(func() error { return a.herdr.Prompt(ctx, name, prompt) }); err != nil {
			undo()
			return fmt.Errorf("prompt: %w", err)
		}
		// A prompt can fail soft: herdr accepts it while the agent is
		// still settling and the keys go nowhere, and recovery can also
		// outlive this request's 20s budget — the request ctx must not
		// cut it. Verify briefly; on no turn, hand the recovery to a
		// background goroutine (the request deadline must not apply) and
		// let the create answer with the bind-time session key.
		if !a.sessionAppeared(ctx, paneID, sessionVerifyWait) {
			a.logger.Warn("prompt unverified, recovering in background", slog.String("agent", name))
			go a.ensureTurnStarted(context.WithoutCancel(ctx), paneID, name, prompt)
		}
	}
	return nil
}

// sessionVerifyWait bounds one check that pi's first turn produced a
// session file.
var sessionVerifyWait = 10 * time.Second

// ensureTurnStarted runs off-request: it verifies pi began its first
// turn and nudges a soft-failed prompt. herdr's queue always delivers
// eventually (observed 30-90s under load), so re-prompting is
// forbidden — a late first delivery plus a re-prompt duplicates the
// turn. The only safe nudge is enter when the draft sits unanswered
// in the editor; entering an empty TUI is a harmless no-op.
func (a *app) ensureTurnStarted(ctx context.Context, paneID, name, prompt string) {
	collapse := strings.Join(strings.Fields(prompt), " ")
	frag := collapse
	if r := []rune(frag); len(r) > 40 {
		frag = string(r[:40])
	}
	for attempt := 0; attempt < 4; attempt++ {
		if a.sessionAppeared(ctx, paneID, sessionVerifyWait) {
			return
		}
		plain := ""
		if read, err := a.herdr.PaneRead(ctx, paneID, "detection", 10); err == nil {
			plain = strings.Join(strings.Fields(ansiRe.ReplaceAllString(read.Text, "")), " ")
		}
		if strings.Contains(plain, frag) {
			// The text landed; only the submit-enter was lost.
			if err := herdrRetryErr(func() error { return a.herdr.PaneSendInput(ctx, paneID, "", []string{"enter"}) }); err != nil {
				a.logger.Warn("prompt submit failed", slog.String("error", err.Error()))
				return
			}
		}
	}
	a.logger.Warn("agent never started its first turn", slog.String("agent", name))
}

// waitPaneReady polls the pane until pi's TUI paints its stats
// footer — the moment it starts reading input.
func (a *app) waitPaneReady(ctx context.Context, paneID string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		read, err := a.herdr.PaneRead(ctx, paneID, "detection", 8)
		if err == nil && piReadyRe.MatchString(ansiRe.ReplaceAllString(read.Text, "")) {
			return true
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(400 * time.Millisecond):
		}
	}
}

// sessionAppeared polls until the pane's agent carries a session path
// AND the jsonl exists on disk — herdr reports the path at agent bind,
// long before pi writes the file at turn start, so the path alone
// verifies nothing.
func (a *app) sessionAppeared(ctx context.Context, paneID string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		agents, err := a.herdr.Agents(ctx)
		if err == nil {
			for _, ag := range agents {
				if ag.PaneID == paneID && ag.Session != nil && ag.Session.Value != "" {
					if _, err := os.Stat(ag.Session.Value); err == nil {
						return true
					}
				}
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(400 * time.Millisecond):
		}
	}
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

// hcSpawnName fits agentName's output to hc's terminal-name rules: hc
// rejects names shaped like an address (t7), so a bare alnum name
// gains a pi- prefix.
func hcSpawnName(label string) string {
	name := agentName(label)
	if !strings.ContainsAny(name, "-_.") {
		if name = "pi-" + name; len(name) > 32 {
			name = name[:32]
		}
	}
	return name
}

// createAgentSessionHC spawns pi in a fresh hc terminal, submits the
// first prompt and waits for the bridge's pi.session label plus the
// file itself. A name conflict retries once with a suffix.
func (a *app) createAgentSessionHC(ctx context.Context, label, cwd, prompt string, piArgs []string) (string, string, error) {
	name := hcSpawnName(label)
	t9, err := a.hcDrv.Spawn(ctx, cwd, name, piArgs)
	if err != nil {
		name = hcSpawnName(label + fmt.Sprintf("-%04x", time.Now().UnixNano()&0xffff))
		if t9, err = a.hcDrv.Spawn(ctx, cwd, name, piArgs); err != nil {
			return "", "", fmt.Errorf("hc spawn: %w", err)
		}
	}
	addr := t9.Address
	undo := func() { _ = a.hcDrv.Discard(context.Background(), addr) }
	if prompt != "" {
		if err := a.hcDrv.Prompt(ctx, addr, prompt); err != nil {
			undo()
			return "", "", fmt.Errorf("prompt: %w", err)
		}
		if !a.sessionAppearedHC(ctx, addr, sessionVerifyWait) {
			a.logger.Warn("prompt unverified, recovering in background")
			go a.ensureTurnStartedHC(context.WithoutCancel(ctx), addr, prompt)
		}
	}
	key, err := a.sessionKeyForAddrHC(ctx, addr, undo)
	return key, name, err
}

// resumeSessionHC starts pi again over a closed session's jsonl via
// --session, which loads that exact file, so the phone's key never
// changes and no session discovery is needed.
func (a *app) resumeSessionHC(ctx context.Context, label, cwd, path, text string) (string, error) {
	name := hcSpawnName(label)
	t9, err := a.hcDrv.Spawn(ctx, cwd, name, []string{"--session", path})
	if err != nil {
		name = hcSpawnName(label + fmt.Sprintf("-%04x", time.Now().UnixNano()&0xffff))
		if t9, err = a.hcDrv.Spawn(ctx, cwd, name, []string{"--session", path}); err != nil {
			return "", fmt.Errorf("hc spawn: %w", err)
		}
	}
	addr := t9.Address
	if text != "" {
		if err := a.hcDrv.Prompt(ctx, addr, text); err != nil {
			_ = a.hcDrv.Discard(context.Background(), addr)
			return "", fmt.Errorf("prompt: %w", err)
		}
	}
	return name, nil
}

// sessionAppearedHC polls until the terminal carries a pi.session
// label AND the jsonl exists on disk - the bridge labels at session
// start, long before pi writes the file at turn start.
func (a *app) sessionAppearedHC(ctx context.Context, addr string, d time.Duration) bool {
	deadline := time.Now().Add(d)
	for {
		if p, ok := a.hcDrv.SessionLabel(addr); ok {
			if _, err := os.Stat(p); err == nil {
				return true
			}
		}
		if time.Now().After(deadline) {
			return false
		}
		select {
		case <-ctx.Done():
			return false
		case <-time.After(400 * time.Millisecond):
		}
	}
}

// sessionKeyForAddrHC waits for the spawned pi's session file and
// returns the phone's key for it.
func (a *app) sessionKeyForAddrHC(ctx context.Context, addr string, undo func()) (string, error) {
	deadline := time.Now().Add(agentCreateTimeout - 5*time.Second)
	for {
		if p, ok := a.hcDrv.SessionLabel(addr); ok {
			if _, err := os.Stat(p); err == nil {
				return strings.TrimSuffix(filepath.Base(p), ".jsonl"), nil
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

// ensureTurnStartedHC nudges a soft-failed first prompt off-request:
// hc's Send always appends Enter, so a lost submit means the draft
// sits un-submitted and pressing enter again is the only safe nudge.
func (a *app) ensureTurnStartedHC(ctx context.Context, addr, prompt string) {
	collapse := strings.Join(strings.Fields(prompt), " ")
	frag := collapse
	if r := []rune(frag); len(r) > 40 {
		frag = string(r[:40])
	}
	for attempt := 0; attempt < 4; attempt++ {
		if a.sessionAppearedHC(ctx, addr, sessionVerifyWait) {
			return
		}
		plain := ""
		if read, err := a.hcDrv.PaneRead(ctx, addr, "detection", 10); err == nil {
			plain = strings.Join(strings.Fields(read.Text), " ")
		}
		if strings.Contains(plain, frag) {
			if err := a.hcDrv.PressKeys(ctx, addr, []string{"enter"}); err != nil {
				a.logger.Warn("prompt submit failed", slog.String("error", err.Error()))
				return
			}
		}
	}
	a.logger.Warn("agent never started its first turn")
}
