package quotas

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
	"time"
)

func minimaxSrv(t *testing.T, status int, delay time.Duration, body string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))
}

// minimax.io answers slower than the shared 10s client budget from
// some networks; the fetcher must widen its own deadline instead of
// failing the round. The 4x sleep vs timeout makes the inherited
// budget lose and the widened one win.
func TestMinimaxFetchOutlivesSharedClientTimeout(t *testing.T) {
	body := `{"base_resp":{"status_code":0},"model_remains":[
		{"model_name":"general","current_interval_remaining_percent":70,
		 "end_time":1730000000000,"current_weekly_remaining_percent":50,
		 "weekly_end_time":1730000000000}]}`
	srv := minimaxSrv(t, http.StatusOK, 200*time.Millisecond, body)
	defer srv.Close()

	client := &http.Client{Timeout: 50 * time.Millisecond}
	q := fetchMinimaxQuotaAt(client, srv.URL)
	if q.Error != "" {
		t.Fatalf("expected the fetcher to outlive the shared timeout, got: %q", q.Error)
	}
	if len(q.Windows) != 2 {
		t.Fatalf("windows = %d, want interval + weekly", len(q.Windows))
	}
}

// A 200 with a non-zero base_resp is an API failure, not an empty
// quota: surface the API's own message so the card can say why.
func TestMinimaxFetchSurfacesAPIError(t *testing.T) {
	srv := minimaxSrv(t, http.StatusOK, 0, `{"base_resp":{"status_code":1004,"status_msg":"invalid api key"}}`)
	defer srv.Close()

	q := fetchMinimaxQuotaAt(srv.Client(), srv.URL)
	if q.Error != "invalid api key" {
		t.Fatalf("error = %q, want the API status message", q.Error)
	}
	if len(q.Windows) != 0 {
		t.Fatalf("windows = %d, want none on an API error", len(q.Windows))
	}
}

// The phone renders q.Error verbatim: a deadline miss must read short,
// not as Go's wrapped url.Error with the endpoint URL inside.
func TestMinimaxDeadlineReadsShort(t *testing.T) {
	err := &url.Error{Op: "Get", URL: "https://www.minimax.io/v1/token_plan/remains", Err: context.DeadlineExceeded}
	if got := minimaxDoError(err); got != "timed out" {
		t.Fatalf("minimaxDoError = %q, want a short timeout note", got)
	}
	long := minimaxDoError(&url.Error{Op: "Get", URL: "https://www.minimax.io", Err: errors.New("connection refused")})
	if long != "Get \"https://www.minimax.io\": connection refused" {
		t.Fatalf("other errors pass through: %q", long)
	}
}
