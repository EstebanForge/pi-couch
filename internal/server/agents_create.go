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
	"sync/atomic"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
)

// The home FAB's create flow: pick a folder (or a space), write a
// first prompt, get a running pi session. Client-supplied cwd must
// live under the user's home (the same jail as /api/dirs) so the
// phone cannot spawn agents in arbitrary paths.

// hc spawns need runway: terminal.run, the first status report, and
// the send pipeline's quiet/settle windows stack well past 20s. Var so
// tests can shrink it.
var agentCreateTimeout = 45 * time.Second

// createBudget is the create/resume deadline.
func (a *app) createBudget() time.Duration {
	return agentCreateTimeout
}

// agentName folds a folder basename into a spawn name: start with a
// lowercase letter, only [a-z0-9-_], 1-32 chars. Real folders violate
// every one of those (leading dots, dots, upper case), so anything
// invalid becomes a letter-prefixed folded form.
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

// spaceWire is one space row the new-agent sheet offers.
type spaceWire struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Folder string `json:"folder"`
}

// apiSpaces lists the herdr app's spaces (ADR-0011): the sheet's
// spawn targets. An action surface fails loudly, unlike the passive
// home badges. Unnamed spaces fold to their folder basename, the same
// derived name the app paints.
func (a *app) apiSpaces(w http.ResponseWriter, r *http.Request) {
	if a.hcDrv == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no driver"})
		return
	}
	spaces, err := a.hcDrv.Spaces(r.Context())
	if err != nil {
		a.logger.Error("space list failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "core unreachable"})
		return
	}
	out := make([]spaceWire, 0, len(spaces))
	for _, s := range spaces {
		name := s.Name
		if name == "" {
			name = filepath.Base(s.Folder)
		}
		out = append(out, spaceWire{ID: s.ID, Name: name, Folder: s.Folder})
	}
	writeJSON(w, http.StatusOK, out)
}

// apiAgents creates a pi agent and submits the first prompt,
// answering {key} once pi has a session file (it only appears after
// the first prompt). Three selectors, exactly one required: cwd
// (fresh spawn at that folder), space_id (spawn into the space: its
// folder as cwd, adopted into the app's space tree via labels), or
// label with cwd (the Quick flow: reuse the space named label, else
// spawn at cwd).
func (a *app) apiAgents(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Cwd      string `json:"cwd"`
		SpaceID  string `json:"space_id"`
		Label    string `json:"label"`
		Prompt   string `json:"prompt"`
		Model    string `json:"model"`
		Thinking string `json:"thinking"`
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
	// Exactly one selector: cwd alone, space_id alone, or label with
	// its create-at cwd (Quick). Validation precedes the driver gate,
	// so a bad request 400s even without a core to talk to.
	mode := ""
	switch {
	case req.SpaceID != "" && req.Cwd == "" && req.Label == "":
		mode = "space"
	case req.Label != "" && req.SpaceID == "" && req.Cwd != "":
		mode = "label"
	case req.Cwd != "" && req.SpaceID == "" && req.Label == "":
		mode = "cwd"
	default:
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "exactly one of cwd, space_id, or label with cwd"})
		return
	}
	// Client-supplied folders jail now: the jail is request validation,
	// not a driver concern.
	var jailed string
	if mode != "space" {
		var err error
		jailed, err = a.homeDir(req.Cwd)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
			return
		}
	}
	if a.hcDrv == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no driver"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.createBudget())
	defer cancel()

	// resolveDir resolves the spawn folder (a space's or the jailed
	// client cwd) and returns the adoption labels.
	resolveDir := func() (string, map[string]string, error) {
		switch mode {
		case "space":
			s, ok := a.spaceByID(ctx, req.SpaceID)
			if !ok {
				return "", nil, fmt.Errorf("%w: unknown space", errSpaceDir)
			}
			cwd, err := a.homeDir(s.Folder)
			if err != nil {
				return "", nil, fmt.Errorf("%w: %s", errSpaceDir, s.Folder)
			}
			return cwd, a.spaceLabels(s), nil
		case "label":
			// The Quick flow's label matches a space by name when one
			// exists; an unmatched label keeps the plain spawn.
			if id, ok := a.spaceIDByName(ctx, req.Label); ok {
				if s, ok := a.spaceByID(ctx, id); ok {
					if dir, derr := a.homeDir(s.Folder); derr == nil {
						return dir, a.spaceLabels(s), nil
					}
				}
			}
			return jailed, nil, nil
		default:
			return jailed, nil, nil
		}
	}
	cwd, labels, err := resolveDir()
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	key, name, err := a.createAgentSessionHC(ctx, filepath.Base(cwd), cwd, req.Prompt, piArgs, labels)
	if err != nil {
		a.logger.Error("agent create failed", slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "create failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "agent": name})
}

// errSpaceDir marks a space the phone may not spawn into (unknown
// id, folder outside home).
var errSpaceDir = errors.New("space must be known and live under home")

// spaceByID fetches one space by id from the app document.
func (a *app) spaceByID(ctx context.Context, id string) (hc.Space, bool) {
	spaces, err := a.hcDrv.Spaces(ctx)
	if err != nil {
		return hc.Space{}, false
	}
	for _, s := range spaces {
		if s.ID == id {
			return s, true
		}
	}
	return hc.Space{}, false
}

// spaceIDByName matches the Quick flow's label against space names
// (unnamed spaces fold to their folder basename first).
func (a *app) spaceIDByName(ctx context.Context, label string) (string, bool) {
	spaces, err := a.hcDrv.Spaces(ctx)
	if err != nil {
		return "", false
	}
	for _, s := range spaces {
		name := s.Name
		if name == "" {
			name = filepath.Base(s.Folder)
		}
		if name == label {
			return s.ID, true
		}
	}
	return "", false
}

// spaceLabels builds the adoption labels that place a spawn inside
// the app's space tree: herdr.space plus the space's first tab. No
// tab in the doc means no labels: an id the app does not know would
// spawn a stray unnamed space, and the folder alone is honest.
func (a *app) spaceLabels(s hc.Space) map[string]string {
	if s.FirstTab == "" {
		return nil
	}
	return map[string]string{"herdr.space": s.ID, "herdr.tab": s.FirstTab}
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

// teardown is the create's rollback. disarm retires it once the
// prompt is delivered: hc accepted the text, so every later failure
// must leave the agent running, not kill it (the 2026-11-02 quick
// create died this way - key fetch gave up on a spent budget and
// closed a working agent).
type teardown struct {
	cancel func()
	off    atomic.Bool
}

func newTeardown(cancel func()) *teardown {
	return &teardown{cancel: cancel}
}

func (t *teardown) undo() {
	if t.off.Swap(true) {
		return
	}
	t.cancel()
}

func (t *teardown) disarm() { t.off.Store(true) }

// sessionVerifyWait bounds one check that pi's first turn produced a
// session file.
var sessionVerifyWait = 10 * time.Second

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

// hcNameTaken is the only spawn error worth a rename-and-retry.
func hcNameTaken(err error) bool {
	var hcErr *hc.Error
	return errors.As(err, &hcErr) && hcErr.Data.Kind == "name_taken"
}

// hcSpawnRetryName keeps the suffix inside the core's 32-char cap.
func hcSpawnRetryName(label string) string {
	suffix := fmt.Sprintf("-%04x", time.Now().UnixNano()&0xffff)
	base := hcSpawnName(label)
	if len(base) > 32-len(suffix) {
		base = base[:32-len(suffix)]
	}
	return base + suffix
}

// createAgentSessionHC spawns pi in a fresh hc terminal (optionally
// adopted into the app's space tree via labels), submits the first
// prompt and waits for the bridge's pi.session label plus the file
// itself. A name conflict retries once with a suffix.
func (a *app) createAgentSessionHC(ctx context.Context, label, cwd, prompt string, piArgs []string, labels map[string]string) (string, string, error) {
	name := hcSpawnName(label)
	t9, err := a.hcDrv.Spawn(ctx, cwd, name, piArgs, labels)
	if err != nil && hcNameTaken(err) {
		name = hcSpawnRetryName(label)
		t9, err = a.hcDrv.Spawn(ctx, cwd, name, piArgs, labels)
	}
	if err != nil {
		return "", "", fmt.Errorf("hc spawn: %w", err)
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
	t9, err := a.hcDrv.Spawn(ctx, cwd, name, []string{"--session", path}, nil)
	if err != nil && hcNameTaken(err) {
		name = hcSpawnRetryName(label)
		t9, err = a.hcDrv.Spawn(ctx, cwd, name, []string{"--session", path}, nil)
	}
	if err != nil {
		return "", fmt.Errorf("hc spawn: %w", err)
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

// apiSessionResume restarts a closed session: a fresh hc terminal at
// the session's cwd runs pi --session <jsonl>, and the optional text
// rides along as the first prompt. The jsonl already exists, so the
// answer needs no session discovery - the requested key is the key.
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
	if a.hcDrv == nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "no driver"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.createBudget())
	defer cancel()
	if _, err := a.hcDrv.TargetForPath(ctx, meta.Path); err == nil {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "running"})
		return
	}
	cwd, err := a.homeDir(meta.Cwd)
	if err != nil {
		// The session's project may be gone; pi --session takes an
		// absolute path, so the spawn cwd only needs to be legal.
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
	name, err := a.resumeSessionHC(ctx, label, cwd, meta.Path, strings.TrimSpace(req.Text))
	if err != nil {
		a.logger.Error("resume failed", slog.String("key", key), slog.String("error", err.Error()))
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "resume failed"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"key": key, "agent": name})
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

// sessionKeyForAddrHC waits for the bridge's session label and
// returns the phone's key for it. The label is enough: the jsonl can
// lag a recovered first prompt, and killing a healthy agent for that
// is worse than returning before the file lands.
func (a *app) sessionKeyForAddrHC(ctx context.Context, addr string, undo func()) (string, error) {
	deadline := time.Now().Add(a.createBudget() - 5*time.Second)
	for {
		if p, ok := a.hcDrv.SessionLabel(addr); ok {
			return strings.TrimSuffix(filepath.Base(p), ".jsonl"), nil
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
