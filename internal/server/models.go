package server

import (
	"context"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// modelRow is one pi --list-models line as "provider/model".
type modelRow struct {
	ID string `json:"id"`
}

// The catalog is static per pi release: load it once and refilter in
// memory; running pi takes seconds.
var (
	modelCacheMu   sync.Mutex
	modelCacheRows []modelRow
	modelCacheAt   time.Time
)

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

func loadModels(ctx context.Context) ([]modelRow, error) {
	modelCacheMu.Lock()
	defer modelCacheMu.Unlock()
	if modelCacheRows != nil && time.Since(modelCacheAt) < time.Hour {
		return modelCacheRows, nil
	}
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
	modelCacheRows = parseModelList(string(out))
	modelCacheAt = time.Now()
	return modelCacheRows, nil
}

// parseModelList reads the TSV table: provider, model, context, max-out,
// thinking, images. Rows with fewer than two columns are junk, not data.
func parseModelList(tsv string) []modelRow {
	var rows []modelRow
	for _, line := range strings.Split(tsv, "\n") {
		f := strings.Fields(line)
		if len(f) < 2 || f[0] == "provider" {
			continue
		}
		rows = append(rows, modelRow{ID: f[0] + "/" + f[1]})
	}
	return rows
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

func (a *app) models(w http.ResponseWriter, r *http.Request) {
	rows, err := loadModels(r.Context())
	if err != nil {
		a.logger.Error("list-models", "err", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "model catalog unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, filterModels(rows, r.URL.Query().Get("q"), 25))
}
