package server

import (
	"context"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/EstebanForge/couch-pi/internal/store"
)

//go:embed models_dump.mjs
var modelsDumpScript string

// modelRow is one pi --list-models line as "provider/model".
type modelRow struct {
	ID      string   `json:"id"`
	Context int      `json:"context,omitempty"`
	Levels  []string `json:"levels,omitempty"`
}

// errDegraded marks a run where --list-models succeeded but the
// thinking dump did not. The rows are still served — the picker
// falls back to the full ladder — but refresh must keep them out of
// the cache file so one bad dump cannot hide levels for a week.
var errDegraded = errors.New("thinking dump unavailable")

// The catalog is static per pi release but pi --list-models takes
// seconds, so it is cached three deep: memory for an hour, a state-dir
// file for ttl, and on a pi failure the file at any age. A restart or
// a broken pi must not blank the picker while a copy exists.
type modelsCache struct {
	mu   sync.Mutex
	rows []modelRow
	at   time.Time
	path string
	ttl  time.Duration
	run  func(context.Context) ([]modelRow, error)
}

func newModelsCache(path string) *modelsCache {
	dir := filepath.Dir(path)
	return &modelsCache{
		path: path,
		ttl:  7 * 24 * time.Hour,
		run:  func(ctx context.Context) ([]modelRow, error) { return runPiModels(ctx, dir) },
	}
}

func runPiModels(ctx context.Context, scriptDir string) ([]modelRow, error) {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	bin, err := piBinary()
	if err != nil {
		return nil, err
	}
	out, err := exec.CommandContext(ctx, bin, "--list-models").Output()
	if err != nil {
		return nil, err
	}
	rows := parseModelList(string(out))
	// Levels ride best-effort: a dump failure (pi internals changed,
	// node missing) leaves rows without levels and the picker falls
	// back to the full list, exactly as before this existed.
	dump, err := dumpThinkingLevels(ctx, scriptDir)
	if err != nil {
		log.Printf("models: %v", err)
		return rows, errDegraded
	}
	applyThinkingDump(rows, dump)
	return rows, nil
}

// thinkingDumpRow is one entry of the models_dump.mjs output.
type thinkingDumpRow struct {
	ID     string   `json:"id"`
	Levels []string `json:"levels"`
}

// applyThinkingDump merges the dump onto the table rows by id. Rows
// the dump does not know stay bare; the phone treats missing levels
// as "show everything".
func applyThinkingDump(rows []modelRow, dump []byte) {
	var parsed []thinkingDumpRow
	if err := json.Unmarshal(dump, &parsed); err != nil {
		return
	}
	byID := make(map[string]thinkingDumpRow, len(parsed))
	for _, d := range parsed {
		byID[d.ID] = d
	}
	for i := range rows {
		if d, ok := byID[rows[i].ID]; ok {
			rows[i].Levels = d.Levels
		}
	}
}

// dumpThinkingLevels runs the embedded dump script under pi's own
// node against pi's current managed release, so the level semantics
// come from pi's code. The script imports pi's model runtime, which
// merges the built-in provider catalogs with the user's models.json
// overrides — the same models the terminal's own picker offers.
func dumpThinkingLevels(ctx context.Context, scriptDir string) ([]byte, error) {
	root, err := piReleaseRoot()
	if err != nil {
		return nil, err
	}
	node, err := piNode()
	if err != nil {
		return nil, err
	}
	script, err := writeModelsDumpScript(scriptDir)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, node, script, root).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) && len(ee.Stderr) > 0 {
			return nil, fmt.Errorf("thinking dump: %w: %s", err, ee.Stderr)
		}
		return nil, fmt.Errorf("thinking dump: %w", err)
	}
	if !json.Valid(out) {
		return nil, fmt.Errorf("thinking dump: output is not JSON")
	}
	return out, nil
}

// piReleaseRoot resolves ~/.pi/agent/install/releases/<current-version>,
// honoring PI_MANAGED_INSTALL_ROOT the way pi's own launcher does.
func piReleaseRoot() (string, error) {
	root := os.Getenv("PI_MANAGED_INSTALL_ROOT")
	if root == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		root = filepath.Join(home, ".pi", "agent", "install")
	}
	ver, err := os.ReadFile(filepath.Join(root, "current-version"))
	if err != nil {
		return "", err
	}
	v := strings.TrimSpace(string(ver))
	if v == "" {
		return "", fmt.Errorf("empty pi version file")
	}
	return filepath.Join(root, "releases", v), nil
}

// piNode prefers the node binary the pi installer manages, falling
// back to whatever node is on PATH.
func piNode() (string, error) {
	dataDir := os.Getenv("XDG_DATA_HOME")
	if dataDir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dataDir = filepath.Join(home, ".local", "share")
	}
	managed := filepath.Join(dataDir, "pi-node", "current", "bin", "node")
	if st, err := os.Stat(managed); err == nil && !st.IsDir() {
		return managed, nil
	}
	return exec.LookPath("node")
}

// writeModelsDumpScript materializes the embedded dump next to the
// models cache file; pi's node needs a real file to import from.
func writeModelsDumpScript(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "models_dump.mjs")
	if err := os.WriteFile(path, []byte(modelsDumpScript), 0o644); err != nil {
		return "", err
	}
	return path, nil
}

func (c *modelsCache) load(ctx context.Context) ([]modelRow, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rows != nil && time.Since(c.at) < time.Hour {
		return c.rows, nil
	}
	fileRows, fileAt, err := readModelFile(c.path)
	if err == nil && time.Since(fileAt) < c.ttl {
		c.rows, c.at = fileRows, time.Now()
		return c.rows, nil
	}
	fresh, err := c.run(ctx)
	if err != nil {
		// Stale beats blank: the picker tolerates an old catalog.
		if fileRows != nil {
			return fileRows, nil
		}
		return nil, err
	}
	c.rows, c.at = fresh, time.Now()
	writeModelFile(c.path, fresh)
	return c.rows, nil
}

// refresh re-runs the catalog fetch regardless of cache age, so a
// provider or model-scope change on the machine shows without waiting
// out the file TTL or restarting. On failure the last good rows are
// returned alongside the error; the caller decides how to surface it.
func (c *modelsCache) refresh(ctx context.Context) ([]modelRow, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	fresh, err := c.run(ctx)
	if err != nil {
		if errors.Is(err, errDegraded) {
			c.rows, c.at = fresh, time.Now()
			return c.rows, nil
		}
		return c.rows, err
	}
	c.rows, c.at = fresh, time.Now()
	writeModelFile(c.path, fresh)
	return c.rows, nil
}

func readModelFile(path string) ([]modelRow, time.Time, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, time.Time{}, err
	}
	var doc struct {
		V         int        `json:"v"`
		FetchedAt time.Time  `json:"fetched_at"`
		Rows      []modelRow `json:"rows"`
	}
	// v2 rows carry the thinking dump; v1 files predate it and must
	// re-run rather than serve a level-less picker forever.
	if err := json.Unmarshal(b, &doc); err != nil || doc.V != 2 || len(doc.Rows) == 0 {
		return nil, time.Time{}, err
	}
	return doc.Rows, doc.FetchedAt, nil
}

func writeModelFile(path string, rows []modelRow) {
	doc := struct {
		V         int        `json:"v"`
		FetchedAt time.Time  `json:"fetched_at"`
		Rows      []modelRow `json:"rows"`
	}{V: 2, FetchedAt: time.Now(), Rows: rows}
	b, err := json.Marshal(doc)
	if err != nil {
		return
	}
	if err := store.WriteFileAtomic(path, b, 0o600); err != nil {
		return
	}
}

// piBinary resolves the pi CLI for list-models. The systemd user unit does
// not carry ~/.local/bin on PATH, hence the explicit fallback.
func piBinary() (string, error) {
	if p, err := exec.LookPath("pi"); err == nil {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".local", "bin", "pi"), nil
}

// parseModelList reads the TSV table: provider, model, context, max-out,
// thinking, images. Rows with fewer than two columns are junk, not data.
// The context column is pi's compact form: 1M, 300K, 128K, or digits.
func parseModelList(tsv string) []modelRow {
	var rows []modelRow
	for _, line := range strings.Split(tsv, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] == "provider" {
			continue
		}
		ctx := 0
		if len(f) > 2 {
			ctx = parseContextWindow(f[2])
		}
		rows = append(rows, modelRow{ID: f[0] + "/" + f[1], Context: ctx})
	}
	return rows
}

// parseContextWindow expands pi's compact window sizes; anything else
// reads as no window.
func parseContextWindow(s string) int {
	s = strings.ToUpper(strings.TrimSpace(s))
	mult := 1
	switch {
	case strings.HasSuffix(s, "M"):
		mult = 1_000_000
		s = strings.TrimSuffix(s, "M")
	case strings.HasSuffix(s, "K"):
		mult = 1_000
		s = strings.TrimSuffix(s, "K")
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return 0
	}
	return n * mult
}

// filterModels substring-matches the query against ids, capped.
func filterModels(rows []modelRow, q string, cap int) []modelRow {
	q = strings.ToLower(q)
	out := make([]modelRow, 0, cap)
	for _, r := range rows {
		if q == "" || strings.Contains(strings.ToLower(r.ID), q) {
			out = append(out, r)
			if len(out) == cap {
				break
			}
		}
	}
	return out
}

func (a *app) apiModels(w http.ResponseWriter, r *http.Request) {
	rows, err := a.models.load(r.Context())
	if err != nil {
		a.logger.Error("list-models", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "model catalog unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, filterModels(rows, r.URL.Query().Get("q"), 500))
}

func (a *app) apiModelsRefresh(w http.ResponseWriter, r *http.Request) {
	rows, err := a.models.refresh(r.Context())
	if err != nil {
		a.logger.Error("list-models refresh", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "model catalog refresh failed"})
		return
	}
	writeJSON(w, http.StatusOK, rows)
}
