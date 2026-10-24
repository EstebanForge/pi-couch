package quotas

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestQuotasSnapshotSerialization(t *testing.T) {
	snap := QuotasSnapshot{
		UpdatedAt: time.Now(),
		Providers: []ProviderQuota{
			{
				ID:   "claude",
				Name: "Claude",
				Plan: "Pro",
				Windows: []WindowQuota{
					{
						Label:    "Session (5h)",
						Percent:  12.5,
						Unit:     "%",
						ResetsAt: time.Now().Add(2 * time.Hour),
					},
				},
			},
		},
	}

	b, err := json.Marshal(snap)
	if err != nil {
		t.Fatalf("marshal error: %v", err)
	}

	var parsed QuotasSnapshot
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if len(parsed.Providers) != 1 || parsed.Providers[0].ID != "claude" {
		t.Fatalf("unexpected parsed snapshot: %+v", parsed)
	}
}

func TestServiceCaching(t *testing.T) {
	callCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"five_hour":{"utilization":10,"resets_at":"2026-10-24T12:00:00Z"},"seven_day":{"utilization":20,"resets_at":"2026-10-30T12:00:00Z"}}`))
	}))
	defer srv.Close()

	svc := NewService()
	snap1 := svc.Get(false)
	snap2 := svc.Get(false)

	if snap1.UpdatedAt != snap2.UpdatedAt {
		t.Errorf("expected cached snapshot timestamp to match")
	}

	snap3 := svc.Get(true)
	if snap3.UpdatedAt == snap1.UpdatedAt {
		t.Errorf("expected refreshed snapshot timestamp to differ")
	}
}
