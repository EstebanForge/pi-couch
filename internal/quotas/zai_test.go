package quotas

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestFetchZaiQuotaOrdersTokensFirst(t *testing.T) {
	body := `{"code":200,"msg":"ok","data":{"level":"pro","limits":[
		{"type":"TIME_LIMIT","percentage":40,"remaining":61,"nextResetTime":1730000000000},
		{"type":"TOKENS_LIMIT","percentage":12,"remaining":88,"nextResetTime":1730000000000}
	]}}`
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()

	prev := zaiQuotaURL
	zaiQuotaURL = srv.URL
	defer func() { zaiQuotaURL = prev }()
	t.Setenv("ZAI_API_KEY", "test-key")

	q := fetchZaiQuota(srv.Client())
	if q.Error != "" {
		t.Fatalf("error: %s", q.Error)
	}
	if len(q.Windows) != 2 {
		t.Fatalf("windows = %d, want 2", len(q.Windows))
	}
	if q.Windows[0].Label != "Session remaining (5h)" {
		t.Fatalf("first = %q, want Session remaining (5h)", q.Windows[0].Label)
	}
	if q.Windows[1].Label != "Web searches remaining" {
		t.Fatalf("second = %q, want Web searches remaining", q.Windows[1].Label)
	}
	if !strings.Contains(q.Windows[1].Remaining, "61") {
		t.Fatalf("search remaining = %q, want the 61 count", q.Windows[1].Remaining)
	}
}
