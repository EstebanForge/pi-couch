package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/hc"
)

// Spaces (ADR-0011) replace the herdr workspaces across the create
// flow: the sheet lists them, space_id spawns into the space with
// adoption labels, and the Quick flow's label matches a space by name.

func spaceDoc(t *testing.T, f *hcFakeCore, spaces ...map[string]any) {
	t.Helper()
	f.mu.Lock()
	f.docs["herdr-app"] = map[string]any{"spaces": spaces}
	f.mu.Unlock()
}

func TestSpacesEndpointListsTheAppDocument(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, "/x/s1.jsonl")
	wireHC(a, drv)
	spaceDoc(t, f,
		map[string]any{"id": "sp1", "name": "", "folder": proj, "tabs": []any{map[string]any{"id": "tb1"}}},
		map[string]any{"id": "sp2", "name": "Forge", "folder": home},
		map[string]any{"folder": "/idless/is/skipped"},
	)

	req := httptest.NewRequest(http.MethodGet, "/api/spaces", nil)
	req.Host = "127.0.0.1:8090"
	rec := httptest.NewRecorder()
	a.apiSpaces(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("spaces code = %d, want 200", rec.Code)
	}
	var spaces []struct {
		ID     string `json:"id"`
		Name   string `json:"name"`
		Folder string `json:"folder"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &spaces); err != nil {
		t.Fatalf("spaces = %s", rec.Body.String())
	}
	if len(spaces) != 2 {
		t.Fatalf("spaces = %s, want 2 (idless skipped)", rec.Body.String())
	}
	// Unnamed spaces fold to the folder basename, the derived name the
	// app itself paints.
	if spaces[0].ID != "sp1" || spaces[0].Name != "proj" || spaces[0].Folder != proj {
		t.Fatalf("spaces[0] = %+v", spaces[0])
	}
	if spaces[1].ID != "sp2" || spaces[1].Name != "Forge" {
		t.Fatalf("spaces[1] = %+v", spaces[1])
	}
}

// spaceCreate runs the create request in the background and drives
// the fake through a fresh spawn's events.
func spaceCreate(t *testing.T, a *app, body map[string]any, f *hcFakeCore, sessPath string) (int, map[string]string) {
	t.Helper()
	type res struct {
		code int
		body map[string]string
	}
	done := make(chan res, 1)
	go func() {
		rec := hcPost(t, a, "/api/agents", "", body)
		var out map[string]string
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		done <- res{rec.Code, out}
	}()
	spawnReady(t, f, sessPath)
	select {
	case r := <-done:
		return r.code, r.body
	case <-time.After(5 * time.Second):
		t.Fatal("create never answered")
		return 0, nil
	}
}

func TestAPIAgentsSpawnIntoSpaceAdoptsLabels(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	spaceDoc(t, f, map[string]any{
		"id": "sp1", "name": "proj", "folder": proj,
		"tabs": []any{map[string]any{"id": "tb1"}},
	})

	code, body := spaceCreate(t, a, map[string]any{"space_id": "sp1", "prompt": "hello"}, f, sessPath)
	if code != 200 {
		t.Fatalf("space create code = %d (%v), want 200", code, body)
	}
	if body["key"] != "s1" {
		t.Fatalf("key = %q, want s1", body["key"])
	}
	runs := f.runLog()
	if len(runs) != 1 {
		t.Fatalf("runs = %d, want 1", len(runs))
	}
	if runs[0]["cwd"] != proj {
		t.Fatalf("run cwd = %v, want the space folder %s", runs[0]["cwd"], proj)
	}
	labels, _ := runs[0]["labels"].(map[string]any)
	if labels["herdr.space"] != "sp1" || labels["herdr.tab"] != "tb1" {
		t.Fatalf("run labels = %v, want herdr.space=sp1 herdr.tab=tb1", labels)
	}
}

func TestAPIAgentsQuickLabelMatchesASpace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(home, "other")
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	spaceDoc(t, f, map[string]any{
		"id": "sp9", "name": "proj", "folder": proj,
		"tabs": []any{map[string]any{"id": "tb9"}},
	})

	// The Quick flow's label matches the space by name: the spawn lands
	// in the space's folder, not the posted cwd.
	code, body := spaceCreate(t, a, map[string]any{"label": "proj", "cwd": other, "prompt": "go"}, f, sessPath)
	if code != 200 {
		t.Fatalf("quick create code = %d (%v), want 200", code, body)
	}
	runs := f.runLog()
	if len(runs) != 1 || runs[0]["cwd"] != proj {
		t.Fatalf("run = %v, want cwd %s", runs, proj)
	}
	labels, _ := runs[0]["labels"].(map[string]any)
	if labels["herdr.space"] != "sp9" {
		t.Fatalf("labels = %v, want the space adoption", labels)
	}
}

func TestAPIAgentsUnknownSpaceRefused(t *testing.T) {
	a, _ := hcDialogApp(t)
	drv, _ := startHCFake(t, "/x/s1.jsonl")
	wireHC(a, drv)

	rec := hcPost(t, a, "/api/agents", "", map[string]any{"space_id": "nope", "prompt": "x"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("unknown space create code = %d, want 400", rec.Code)
	}
}

// An unreadable app document is an outage, not a bad request: the
// create must answer 502 so the phone reports the core, not the pick.
func TestAPIAgentsDeadCoreSpaceCreateAnswers502(t *testing.T) {
	a, _ := hcDialogApp(t)
	drv, _ := startHCFake(t, "/x/s1.jsonl")
	wireHC(a, drv)
	a.hcDrv = hc.NewDriver(hc.NewAt(filepath.Join(t.TempDir(), "dead.sock")))

	// No spawn waits here: the create must refuse at the space lookup,
	// before any terminal is started.
	rec := hcPost(t, a, "/api/agents", "", map[string]any{"space_id": "sp1", "prompt": "x"})
	if rec.Code != http.StatusBadGateway {
		t.Fatalf("dead-core space create code = %d (%s), want 502", rec.Code, rec.Body.String())
	}
}

// The jail resolves symlinks: a link inside home that points out is
// refused, one that stays inside is fine (same rule as /api/dirs).
func TestHomeDirRefusesSymlinkEscape(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	inside := filepath.Join(home, "real")
	if err := os.MkdirAll(inside, 0o755); err != nil {
		t.Fatal(err)
	}
	a, _ := hcDialogApp(t)
	if _, err := a.homeDir(inside); err != nil {
		t.Fatalf("plain dir refused: %v", err)
	}
	linkIn := filepath.Join(home, "link-in")
	if err := os.Symlink(inside, linkIn); err != nil {
		t.Fatal(err)
	}
	if _, err := a.homeDir(linkIn); err != nil {
		t.Fatalf("internal symlink refused: %v", err)
	}
	outside := t.TempDir()
	linkOut := filepath.Join(home, "link-out")
	if err := os.Symlink(outside, linkOut); err != nil {
		t.Fatal(err)
	}
	if _, err := a.homeDir(linkOut); err == nil {
		t.Fatal("escaping symlink accepted")
	}
}

// A space's pin folder is the app's maintained association; the
// folder field drifts when a space is re-pointed (live doc,
// 2026-11-19 had every folder field stale). The pin wins both ways:
// the pinned space gains its real folder and gives up the stale one.
func TestInferSpacePinFolderOverridesStaleFolder(t *testing.T) {
	spaces := []hc.Space{
		{ID: "spF", Name: "Forge", Folder: "/h/Dev/EstebanForge", FirstTab: "tbF"},
		{ID: "spW", Name: "Wicket", Folder: "/h/Dev/EstebanForge", PinFolder: "/h/Dev/Wicket", FirstTab: "tbW"},
	}
	if s, ok := inferSpace(spaces, "/h/Dev/Wicket/case"); !ok || s.ID != "spW" {
		t.Fatalf("= %+v ok=%v, want spW by its pin", s, ok)
	}
	if s, ok := inferSpace(spaces, "/h/Dev/EstebanForge/sub"); !ok || s.ID != "spF" {
		t.Fatalf("= %+v ok=%v, want spF (the pinned space moved away)", s, ok)
	}
}

// A pin rescues a space whose folder field is empty.
func TestInferSpacePinRescuesEmptyFolder(t *testing.T) {
	spaces := []hc.Space{{ID: "spP", Folder: "", PinFolder: "/h/Dev/pin-only", FirstTab: "tbP"}}
	if s, ok := inferSpace(spaces, "/h/Dev/pin-only/x"); !ok || s.ID != "spP" {
		t.Fatalf("= %+v ok=%v, want spP", s, ok)
	}
}

// --- ADR-0013: folder inference ---

// inferSpace picks the space a folder belongs to (ADR-0013): the
// space whose folder is the folder itself, else the deepest ancestor
// folder.
func TestInferSpacePicksDeepestOwningFolder(t *testing.T) {
	spaces := []hc.Space{
		{ID: "spQ", Folder: "/h/Dev", FirstTab: "tbQ"},
		{ID: "spB", Folder: "/h/Dev/Baluard", FirstTab: "tbB"},
		{ID: "spW", Folder: "/h/Dev/Wicket", FirstTab: "tbW"},
	}
	if s, ok := inferSpace(spaces, "/h/Dev/Baluard"); !ok || s.ID != "spB" {
		t.Fatalf("exact = %+v ok=%v, want spB", s, ok)
	}
	if s, ok := inferSpace(spaces, "/h/Dev/Baluard/sub/deep"); !ok || s.ID != "spB" {
		t.Fatalf("subfolder = %+v ok=%v, want the deepest parent spB", s, ok)
	}
	if s, ok := inferSpace(spaces, "/h/Dev/Freelance/whooo"); !ok || s.ID != "spQ" {
		t.Fatalf("shallow = %+v ok=%v, want the parent spQ", s, ok)
	}
	if _, ok := inferSpace(spaces, "/elsewhere"); ok {
		t.Fatal("a folder outside every space matched")
	}
}

// A space with no folder and a remote machine's space (its id carries
// the "/" machine prefix and its folders are not this machine's
// paths) never own a local spawn.
func TestInferSpaceSkipsRemoteAndEmptyFolders(t *testing.T) {
	spaces := []hc.Space{
		{ID: "/spRemote", Folder: "/h/Dev", FirstTab: "tbR"},
		{ID: "spEmpty", Folder: "", FirstTab: "tbE"},
		{ID: "spQ", Folder: "/h/Dev", FirstTab: "tbQ"},
	}
	if s, ok := inferSpace(spaces, "/h/Dev/work"); !ok || s.ID != "spQ" {
		t.Fatalf("= %+v ok=%v, want spQ (remote and empty skipped)", s, ok)
	}
}

// A prefix compares at separator boundaries: owning ~/Dev/foo must
// not swallow ~/Dev/foobar.
func TestInferSpacePrefixIsSeparatorAware(t *testing.T) {
	spaces := []hc.Space{{ID: "spF", Folder: "/h/Dev/foo", FirstTab: "tbF"}}
	if s, ok := inferSpace(spaces, "/h/Dev/foobar"); ok {
		t.Fatalf("foobar matched %s", s.ID)
	}
	if s, ok := inferSpace(spaces, "/h/Dev/foo/sub"); !ok || s.ID != "spF" {
		t.Fatalf("sub = %+v ok=%v, want spF", s, ok)
	}
}

// Duplicate folders pick the space that can actually label (a tab),
// then document order.
func TestInferSpaceTiebreakPrefersTabbedThenDocOrder(t *testing.T) {
	tabbed := []hc.Space{
		{ID: "spNoTab", Folder: "/h/Dev"},
		{ID: "spTab", Folder: "/h/Dev", FirstTab: "tb"},
	}
	if s, ok := inferSpace(tabbed, "/h/Dev/x"); !ok || s.ID != "spTab" {
		t.Fatalf("= %+v ok=%v, want the tabbed duplicate", s, ok)
	}
	both := []hc.Space{
		{ID: "spA", Folder: "/h/Dev"},
		{ID: "spB", Folder: "/h/Dev"},
	}
	if s, ok := inferSpace(both, "/h/Dev/x"); !ok || s.ID != "spA" {
		t.Fatalf("= %+v ok=%v, want doc order spA", s, ok)
	}
}

// A tabless deepest match wins and labels nil later; falling through
// to a tabbed parent would place the session in the wrong space.
func TestInferSpaceDeepestTablessWinsWithoutFallback(t *testing.T) {
	spaces := []hc.Space{
		{ID: "spQ", Folder: "/h/Dev", FirstTab: "tbQ"},
		{ID: "spDeep", Folder: "/h/Dev/Baluard"},
	}
	s, ok := inferSpace(spaces, "/h/Dev/Baluard/sub")
	if !ok || s.ID != "spDeep" || s.FirstTab != "" {
		t.Fatalf("= %+v ok=%v, want the tabless deepest, not the parent", s, ok)
	}
}

// Client cwds, session headers and doc folders disagree on symlinks;
// the resolved retry must match through a link on either side.
func TestInferSpaceRetriesSymlinkedFolders(t *testing.T) {
	root := t.TempDir()
	real := filepath.Join(root, "real")
	if err := os.MkdirAll(real, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	if s, ok := inferSpace([]hc.Space{{ID: "spR", Folder: real, FirstTab: "tbR"}}, link); !ok || s.ID != "spR" {
		t.Fatalf("link dir = %+v ok=%v, want spR", s, ok)
	}
	if s, ok := inferSpace([]hc.Space{{ID: "spL", Folder: link, FirstTab: "tbL"}}, real); !ok || s.ID != "spL" {
		t.Fatalf("link folder = %+v ok=%v, want spL", s, ok)
	}
}

// A folder-only create adopts the owning space: the exact folder
// beats the shallower parent, the spawn keeps the client's cwd, and
// the document is read once.
func TestAPIAgentsCwdCreateAdoptsOwningSpace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	sub := filepath.Join(proj, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	spaceDoc(t, f,
		map[string]any{"id": "spQ", "name": "Quick", "folder": home, "tabs": []any{map[string]any{"id": "tbQ"}}},
		map[string]any{"id": "spB", "name": "Baluard", "folder": proj, "tabs": []any{map[string]any{"id": "tbB"}}},
	)

	code, body := spaceCreate(t, a, map[string]any{"cwd": sub, "prompt": "hello"}, f, sessPath)
	if code != 200 {
		t.Fatalf("cwd create code = %d (%v), want 200", code, body)
	}
	runs := f.runLog()
	if len(runs) != 1 || runs[0]["cwd"] != sub {
		t.Fatalf("run = %v, want cwd %s", runs, sub)
	}
	labels, _ := runs[0]["labels"].(map[string]any)
	if labels["herdr.space"] != "spB" || labels["herdr.tab"] != "tbB" {
		t.Fatalf("labels = %v, want the deepest parent spB/tbB", labels)
	}
	if got := f.docGetCount(); got != 1 {
		t.Fatalf("doc.get calls = %d, want 1 per create", got)
	}
}

// The folder field can lie while the space's own pin holds the truth
// (live doc 2026-11-19): a create at the pinned folder adopts that
// space, not the stale folder's owner.
func TestAPIAgentsCwdCreateAdoptsByPinFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	stale := filepath.Join(home, "stale-owner")
	real := filepath.Join(home, "real-home")
	sub := filepath.Join(real, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	spaceDoc(t, f,
		map[string]any{"id": "spStale", "name": "Stale", "folder": stale, "tabs": []any{map[string]any{"id": "tbS"}}},
		map[string]any{"id": "spReal", "name": "Real", "folder": stale, "tabs": []any{map[string]any{
			"id":   "tbR",
			"root": map[string]any{"children": []any{map[string]any{"type": "block", "content": map[string]any{"mod": "space-pin", "props": map[string]any{"folder": real, "space": "spReal"}}}}},
		}}},
	)

	code, body := spaceCreate(t, a, map[string]any{"cwd": sub, "prompt": "hello"}, f, sessPath)
	if code != 200 {
		t.Fatalf("cwd create code = %d (%v), want 200", code, body)
	}
	labels, _ := f.runLog()[0]["labels"].(map[string]any)
	if labels["herdr.space"] != "spReal" || labels["herdr.tab"] != "tbR" {
		t.Fatalf("labels = %v, want the pin's space spReal/tbR", labels)
	}
}

// A folder outside every space keeps the plain unlabeled spawn.
func TestAPIAgentsCwdCreateOutsideSpacesStaysUnlabeled(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	lone := filepath.Join(home, "lone")
	if err := os.MkdirAll(lone, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	spaceDoc(t, f, map[string]any{"id": "spB", "name": "Baluard", "folder": proj, "tabs": []any{map[string]any{"id": "tbB"}}})

	code, body := spaceCreate(t, a, map[string]any{"cwd": lone, "prompt": "hello"}, f, sessPath)
	if code != 200 {
		t.Fatalf("create code = %d (%v), want 200", code, body)
	}
	labels, _ := f.runLog()[0]["labels"].(map[string]any)
	if len(labels) != 0 {
		t.Fatalf("labels = %v, want none", labels)
	}
}

// A Quick label that names no space falls back to owning-folder
// inference instead of dropping the spawn unplaced.
func TestAPIAgentsQuickUnmatchedLabelAdoptsByFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	sub := filepath.Join(proj, "pkg")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	spaceDoc(t, f, map[string]any{"id": "spB", "name": "Baluard", "folder": proj, "tabs": []any{map[string]any{"id": "tbB"}}})

	code, body := spaceCreate(t, a, map[string]any{"label": "nope", "cwd": sub, "prompt": "go"}, f, sessPath)
	if code != 200 {
		t.Fatalf("quick create code = %d (%v), want 200", code, body)
	}
	labels, _ := f.runLog()[0]["labels"].(map[string]any)
	if labels["herdr.space"] != "spB" || labels["herdr.tab"] != "tbB" {
		t.Fatalf("labels = %v, want the folder adoption", labels)
	}
}

// An unreadable document must not break the create: the spawn fails
// open to the plain folder path.
func TestAPIAgentsCwdCreateSurvivesDocFailure(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.failDocs = true
	f.mu.Unlock()
	wireHC(a, drv)

	code, body := spaceCreate(t, a, map[string]any{"cwd": proj, "prompt": "hello"}, f, sessPath)
	if code != 200 {
		t.Fatalf("create code = %d (%v), want 200", code, body)
	}
	labels, _ := f.runLog()[0]["labels"].(map[string]any)
	if len(labels) != 0 {
		t.Fatalf("labels = %v, want none", labels)
	}
}

// A stalled document read rides its own short budget: the create
// still answers, unlabeled, without eating the spawn's runway.
func TestAPIAgentsCwdCreateShrugsAtSlowDoc(t *testing.T) {
	old := inferBudget
	inferBudget = 200 * time.Millisecond
	t.Cleanup(func() { inferBudget = old })
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.docsDelay = 5 * time.Second
	f.mu.Unlock()
	wireHC(a, drv)

	code, body := spaceCreate(t, a, map[string]any{"cwd": proj, "prompt": "hello"}, f, sessPath)
	if code != 200 {
		t.Fatalf("create code = %d (%v), want 200", code, body)
	}
	labels, _ := f.runLog()[0]["labels"].(map[string]any)
	if len(labels) != 0 {
		t.Fatalf("labels = %v, want none", labels)
	}
}

// Any create without a prompt is a refusal: pi writes the session
// file only at turn start, so a promptless agent could never surface
// on the home list.
func TestAPIAgentsRequirePrompt(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, "couchpi-fab-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	a := &app{logger: testLogger()}
	for _, body := range []string{
		`{"cwd":"` + dir + `"}`,
		`{"space_id":"sp1"}`,
		`{"label":"x","cwd":"` + dir + `","prompt":"   "}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.apiAgents(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: code=%d, want 400", body, rec.Code)
		}
	}
}

func TestAPIAgentsRejectsAmbiguousSelector(t *testing.T) {
	a := &app{logger: testLogger()}
	for _, body := range []string{
		`{"space_id":"sp1","cwd":"/tmp","prompt":"hi"}`,
		`{"space_id":"sp1","label":"x","prompt":"hi"}`,
		`{"label":"x","cwd":"/tmp","space_id":"sp1","prompt":"hi"}`,
		`{"space_id":"sp1","label":"x","cwd":"/tmp","prompt":"hi"}`,
	} {
		req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(body))
		req.Host = "127.0.0.1:8090"
		rec := httptest.NewRecorder()
		a.apiAgents(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %s: code=%d, want 400", body, rec.Code)
		}
	}
}

// seedModelsFile primes the models cache file so create-time model
// validation has a catalog to match against.
func seedModelsFile(t *testing.T, a *app, ids ...string) {
	t.Helper()
	rows := make([]modelRow, len(ids))
	for i, id := range ids {
		rows[i] = modelRow{ID: id}
	}
	b, err := json.Marshal(map[string]any{
		"v":          2,
		"fetched_at": time.Now().UTC(),
		"rows":       rows,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(a.models.path, b, 0o600); err != nil {
		t.Fatal(err)
	}
}

// The create panel can pin a model and thinking level; they ride the
// spawn line as one pi --model flag.
func TestAPIAgentsCarriesModelArgs(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	proj := filepath.Join(home, "proj")
	if err := os.MkdirAll(proj, 0o755); err != nil {
		t.Fatal(err)
	}
	sessPath := filepath.Join(home, ".pi", "agent", "sessions", "s1.jsonl")

	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, sessPath)
	f.mu.Lock()
	f.snap = func() []map[string]any { return nil }
	f.mu.Unlock()
	wireHC(a, drv)
	seedModelsFile(t, a, "zai/glm-5.3-flash", "zai/glm-5.3")

	code, body := spaceCreate(t, a, map[string]any{"cwd": proj, "prompt": "go", "model": "zai/glm-5.3-flash", "thinking": "high"}, f, sessPath)
	if code != 200 {
		t.Fatalf("create code = %d (%v), want 200", code, body)
	}
	line, _ := f.runLog()[0]["line"].(string)
	if !strings.Contains(line, "--model") || !strings.Contains(line, "zai/glm-5.3-flash:high") {
		t.Fatalf("run line = %q, want the --model flag with thinking", line)
	}
}

// Bad model or thinking picks must 400 before any spawn: a bad pi
// flag exits pi and the create would otherwise time out waiting for
// an agent that never binds.
func TestAPIAgentsRejectsBadModelThinking(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp(home, "couchpi-fab-*")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	a, _ := hcDialogApp(t)
	seedModelsFile(t, a, "zai/glm-5.3-flash")

	cases := []struct {
		name string
		body string
		want string
	}{
		{"unknown model", `{"label":"Forge","cwd":"` + dir + `","prompt":"go","model":"nope/x","thinking":"high"}`, "unknown model"},
		{"bad thinking", `{"label":"Forge","cwd":"` + dir + `","prompt":"go","model":"zai/glm-5.3-flash","thinking":"turbo"}`, "bad thinking"},
		{"thinking without model", `{"label":"Forge","cwd":"` + dir + `","prompt":"go","thinking":"high"}`, "thinking requires a model"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/agents", strings.NewReader(tc.body))
			req.Host = "127.0.0.1:8090"
			rec := httptest.NewRecorder()
			a.apiAgents(rec, req)
			if rec.Code != http.StatusBadRequest {
				t.Fatalf("code=%d body=%s", rec.Code, rec.Body.String())
			}
			if !strings.Contains(rec.Body.String(), tc.want) {
				t.Fatalf("body=%s want substring %s", rec.Body.String(), tc.want)
			}
		})
	}
}

// The prompt can fail soft: hc accepted it while pi settles and the
// keys went nowhere. When the draft sits in the editor the recovery
// presses enter; it never re-sends the prompt.
func TestEnsureTurnStartedHCSubmitsDraft(t *testing.T) {
	oldWait := sessionVerifyWait
	sessionVerifyWait = 300 * time.Millisecond
	t.Cleanup(func() { sessionVerifyWait = oldWait })
	a, _ := hcDialogApp(t)
	// Paint: the draft "go" sits in pi's editor.
	drv, f := startHCFake(t, "/x/none.jsonl", "editor\ngo\n0.0%/1.0M\n")
	wireHC(a, drv)
	a.ensureTurnStartedHC(context.Background(), "t1", "go")
	enters := 0
	for _, k := range f.keyLog() {
		for _, key := range k {
			if key == "enter" {
				enters++
			}
		}
	}
	if enters == 0 {
		t.Fatal("no corrective enter pressed")
	}
	if len(f.sendLog()) != 0 {
		t.Fatal("the recovery re-sent the prompt")
	}
}

// A lost-looking prompt must never be sent twice: without the draft
// on screen the recovery presses nothing.
func TestEnsureTurnStartedHCCannotReprompt(t *testing.T) {
	oldWait := sessionVerifyWait
	sessionVerifyWait = 300 * time.Millisecond
	t.Cleanup(func() { sessionVerifyWait = oldWait })
	a, _ := hcDialogApp(t)
	drv, f := startHCFake(t, "/x/none.jsonl", "pi ready\nnobody home\n")
	wireHC(a, drv)
	a.ensureTurnStartedHC(context.Background(), "t1", "tell me a story")
	if n := len(f.keyLog()); n != 0 {
		t.Fatalf("keys = %d, want 0", n)
	}
}
