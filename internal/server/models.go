package server

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"
)

// modelRow is one pi --list-models line as "provider/model".
type modelRow struct {
	ID      string `json:"id"`
	Context int    `json:"context,omitempty"`
}

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
	return &modelsCache{
		path: path,
		ttl:  7 * 24 * time.Hour,
		run:  runPiModels,
	}
}

func runPiModels(ctx context.Context) ([]modelRow, error) {
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
	return parseModelList(string(out)), nil
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
		FetchedAt time.Time  `json:"fetched_at"`
		Rows      []modelRow `json:"rows"`
	}
	if err := json.Unmarshal(b, &doc); err != nil || len(doc.Rows) == 0 {
		return nil, time.Time{}, err
	}
	return doc.Rows, doc.FetchedAt, nil
}

func writeModelFile(path string, rows []modelRow) {
	doc := struct {
		FetchedAt time.Time  `json:"fetched_at"`
		Rows      []modelRow `json:"rows"`
	}{FetchedAt: time.Now(), Rows: rows}
	b, err := json.Marshal(doc)
	if err != nil {
		return
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return
	}
	_ = os.Rename(tmp, path)
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
