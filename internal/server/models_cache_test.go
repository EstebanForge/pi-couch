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

func TestParseModelListContext(t *testing.T) {
	tsv := "provider\tmodel\tcontext\tmax-out\tthinking\timages\n" +
		"zai\tglm-5\t1M\t32000\tyes\tyes\n" +
		"p\tm2\t300K\t1\t1\t1\n" +
		"p\tm3\t200000\t1\t1\t1\n" +
		"p\tm4\tjunk\t1\t1\t1\n" +
		"p\tm5\n"
	rows := parseModelList(tsv)
	if len(rows) != 5 {
		t.Fatalf("rows %+v", rows)
	}
	if rows[0].ID != "zai/glm-5" || rows[0].Context != 1_000_000 {
		t.Fatalf("rows[0] = %+v, want context 1000000 from 1M", rows[0])
	}
	if rows[1].ID != "p/m2" || rows[1].Context != 300_000 {
		t.Fatalf("rows[1] = %+v, want context 300000 from 300K", rows[1])
	}
	if rows[2].ID != "p/m3" || rows[2].Context != 200000 {
		t.Fatalf("rows[2] = %+v, want plain digits through", rows[2])
	}
	if rows[3].ID != "p/m4" || rows[3].Context != 0 {
		t.Fatalf("rows[3] = %+v, want context 0 on junk", rows[3])
	}
	if rows[4].ID != "p/m5" || rows[4].Context != 0 {
		t.Fatalf("rows[4] = %+v, want context 0 when absent", rows[4])
	}
}

func TestModelsCacheRefreshBypassesCache(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	cacheRows(t, path, time.Now(), "p/old")
	c := newModelsCache(path)
	if _, err := c.load(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.run = func(context.Context) ([]modelRow, error) {
		return []modelRow{{ID: "p/new"}}, nil
	}
	rows, err := c.refresh(context.Background())
	if err != nil || len(rows) != 1 || rows[0].ID != "p/new" {
		t.Fatalf("rows %+v err=%v, want the fresh fetch", rows, err)
	}
	// The file must follow, or the next process restart serves the old catalog.
	fileRows, _, err := readModelFile(path)
	if err != nil || len(fileRows) != 1 || fileRows[0].ID != "p/new" {
		t.Fatalf("file rows %+v err=%v, want the fresh fetch persisted", fileRows, err)
	}
}

func TestModelsCacheRefreshFailureKeepsRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "models.json")
	cacheRows(t, path, time.Now(), "p/old")
	c := newModelsCache(path)
	if _, err := c.load(context.Background()); err != nil {
		t.Fatal(err)
	}
	c.run = func(context.Context) ([]modelRow, error) {
		return nil, errors.New("pi exploded")
	}
	rows, err := c.refresh(context.Background())
	if err == nil {
		t.Fatal("want the fetch error surfaced")
	}
	if len(rows) != 1 || rows[0].ID != "p/old" {
		t.Fatalf("rows %+v, want the last good catalog kept", rows)
	}
}

func TestAPIModelsRefreshEndpoint(t *testing.T) {
	a, _, h := streamTestApp(t)
	cacheRows(t, a.models.path, time.Now(), "p/old")
	a.models.run = func(context.Context) ([]modelRow, error) {
		return []modelRow{{ID: "p/new"}}, nil
	}
	req := httptest.NewRequest(http.MethodPost, "/api/models/refresh", nil)
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
	if len(rows) != 1 || rows[0].ID != "p/new" {
		t.Fatalf("rows %+v", rows)
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
