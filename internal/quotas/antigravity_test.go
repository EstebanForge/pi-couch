package quotas

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The captured shape of `agy -p "/quota" --output-format json` (wrapper
// keys trimmed to what the parser consumes).
const agyCLIJSON = `{"description":"quota report","groups":[
 {"name":"Gemini Models","description":"…","buckets":[
   {"id":"gemini-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":0.5455263257026672,"reset_time":"2026-10-30T23:40:02Z"},
   {"id":"gemini-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":0.9848448038101196,"reset_time":"2026-10-27T22:42:59Z"}]},
 {"name":"Claude and GPT models","description":"…","buckets":[
   {"id":"3p-weekly","name":"Weekly Limit Remaining","window":"weekly","remaining_fraction":1,"reset_time":"2026-11-03T17:49:19Z"},
   {"id":"3p-5h","name":"Five Hour Limit Remaining","window":"5h","remaining_fraction":1,"reset_time":"2026-10-27T22:49:19Z"}]},
 {"name":"Gemini 4 Argon","description":"…","buckets":[
   {"id":"gemini-argon-wtu-weekly","name":"Gemini 4 Argon Weekly Limit","window":"weekly","remaining_fraction":1,"reset_time":"2026-11-03T17:49:19Z"}]}]}`

func TestParseAgyCLIQuota(t *testing.T) {
	windows := parseAgyCLIQuota([]byte(agyCLIJSON))
	if len(windows) != 5 {
		t.Fatalf("windows = %d, want 5", len(windows))
	}
	want := []struct {
		label  string
		used   float64
		resets string
	}{
		{"Gemini weekly", 45.4, "2026-10-30T23:40:02Z"},
		{"Gemini 5h", 1.5, "2026-10-27T22:42:59Z"},
		{"Claude/GPT weekly", 0, "2026-11-03T17:49:19Z"},
		{"Claude/GPT 5h", 0, "2026-10-27T22:49:19Z"},
		{"Argon weekly", 0, "2026-11-03T17:49:19Z"},
	}
	for i, w := range want {
		got := windows[i]
		if got.Label != w.label {
			t.Errorf("windows[%d] label = %q, want %q", i, got.Label, w.label)
		}
		if diff := got.Percent - w.used; diff > 0.15 || diff < -0.15 {
			t.Errorf("windows[%d] used = %.2f, want ~%.2f", i, got.Percent, w.used)
		}
		wantReset, _ := time.Parse(time.RFC3339, w.resets)
		if !got.ResetsAt.Equal(wantReset) {
			t.Errorf("windows[%d] resets = %v, want %v", i, got.ResetsAt, wantReset)
		}
	}
}

func TestParseAgyCLIQuotaJunkIsNil(t *testing.T) {
	if parseAgyCLIQuota([]byte("not json")) != nil {
		t.Fatal("junk should parse nil")
	}
	if parseAgyCLIQuota([]byte(`{"groups":[]}`)) != nil {
		t.Fatal("no groups should parse nil")
	}
}

func TestFetchAgyQuotaPrefersCLI(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "agy")
	script := "#!/bin/sh\ncat <<'EOF'\n" + agyCLIJSON + "\nEOF\n"
	if err := os.WriteFile(bin, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	calls := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(500)
	}))
	defer srv.Close()

	q := fetchAntigravityQuotaAt(srv.Client(), srv.URL, srv.URL, bin)
	if q.Error != "" {
		t.Fatalf("error = %q", q.Error)
	}
	if calls != 0 {
		t.Fatalf("summary endpoint hit %d times, want 0 (CLI carries all pools)", calls)
	}
	if len(q.Windows) != 5 {
		t.Fatalf("windows = %d, want 5", len(q.Windows))
	}
}

func TestFetchAgyQuotaFallsBackToSummary(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/token":
			_ = json.NewEncoder(w).Encode(googleTokenResp{AccessToken: "tok", ExpiresIn: 3600})
		case "/quota":
			_, _ = w.Write([]byte(`{"groups":[{"displayName":"Claude and GPT models","buckets":[` +
				`{"displayName":"Weekly Limit Remaining","window":"weekly","resetTime":"2026-11-03T17:49:19Z","remainingFraction":1},` +
				`{"displayName":"Five Hour Limit Remaining","window":"5h","resetTime":"2026-10-27T22:49:19Z","remainingFraction":1}]}]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	home := t.TempDir()
	dir := filepath.Join(home, ".gemini", "antigravity-acp")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	creds := `{"client_id":"c","client_secret":"s","refresh_token":"r"}`
	if err := os.WriteFile(filepath.Join(dir, "acp_token.json"), []byte(creds), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	bin := filepath.Join(t.TempDir(), "agy")
	if err := os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}

	q := fetchAntigravityQuotaAt(srv.Client(), srv.URL+"/quota", srv.URL+"/token", bin)
	if q.Error != "" {
		t.Fatalf("error = %q", q.Error)
	}
	if len(q.Windows) != 2 {
		t.Fatalf("windows = %d, want 2 from the summary fallback", len(q.Windows))
	}
	if !strings.Contains(q.Windows[0].Label, "Weekly") {
		t.Fatalf("label = %q", q.Windows[0].Label)
	}
}
