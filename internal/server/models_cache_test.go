package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func cacheRows(t *testing.T, path string, fetchedAt time.Time, ids ...string) {
	t.Helper()
	doc := struct {
		FetchedAt time.Time  `json:"fetched_at"`
		Rows      []modelRow `json:"rows"`
	}{FetchedAt: fetchedAt}
	for _, id := range ids {
		doc.Rows = append(doc.Rows, modelRow{ID: id})
	}
	b, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestModelsCacheFreshFileSkipsPi(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	cacheRows(t, path, time.Now(), "p/one", "p/two")
	ran := false
	c := newModelsCache(path)
	c.ttl = 7 * 24 * time.Hour
	c.run = func(context.Context) ([]modelRow, error) {
		ran = true
		return nil, errors.New("pi must not run")
	}
	rows, err := c.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if ran {
		t.Fatal("pi ran despite a fresh cache file")
	}
	if len(rows) != 2 || rows[1].ID != "p/two" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestModelsCacheStaleFileRunsPi(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	cacheRows(t, path, time.Now().Add(-8*24*time.Hour), "p/old")
	c := newModelsCache(path)
	c.run = func(context.Context) ([]modelRow, error) {
		return []modelRow{{ID: "p/new"}}, nil
	}
	rows, err := c.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "p/new" {
		t.Fatalf("rows %+v", rows)
	}
	// The rewrite must refresh fetched_at: a second load with pi down
	// now serves the file for the next ttl window.
	c.run = func(context.Context) ([]modelRow, error) {
		return nil, errors.New("pi down")
	}
	rows, err = c.load(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ID != "p/new" {
		t.Fatalf("second load: %+v err=%v", rows, err)
	}
}

func TestModelsCachePiFailureServesStaleFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	old := time.Now().Add(-30 * 24 * time.Hour)
	cacheRows(t, path, old, "p/stale")
	c := newModelsCache(path)
	c.run = func(context.Context) ([]modelRow, error) {
		return nil, errors.New("pi exploded")
	}
	rows, err := c.load(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ID != "p/stale" {
		t.Fatalf("rows %+v", rows)
	}
}

func TestModelsCachePiFailureNoFile(t *testing.T) {
	c := newModelsCache(filepath.Join(t.TempDir(), "models.json"))
	c.run = func(context.Context) ([]modelRow, error) {
		return nil, errors.New("pi exploded")
	}
	if _, err := c.load(context.Background()); err == nil {
		t.Fatal("want an error with nothing to serve")
	}
}

func TestModelsCacheCorruptFileRunsPi(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	if err := os.WriteFile(path, []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	c := newModelsCache(path)
	c.run = func(context.Context) ([]modelRow, error) {
		return []modelRow{{ID: "p/fresh"}}, nil
	}
	rows, err := c.load(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ID != "p/fresh" {
		t.Fatalf("rows %+v err=%v", rows, err)
	}
}

func TestAPIModelsUsesCache(t *testing.T) {
	a, _, h := streamTestApp(t)
	cacheRows(t, a.models.path, time.Now(),
		"zai/glm-5", "antigravity/gemini-4-argon", "zai/glm-air")
	req := httptest.NewRequest(http.MethodGet, "/api/models?q=glm", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("code %d body %s", rec.Code, rec.Body.String())
	}
	var rows []modelRow
	if err := json.Unmarshal(rec.Body.Bytes(), &rows); err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 || rows[0].ID != "zai/glm-5" || rows[1].ID != "zai/glm-air" {
		t.Fatalf("rows %+v", rows)
	}
}
