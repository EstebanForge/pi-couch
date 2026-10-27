package quotas

import (
	"errors"
	"net/http"
	"testing"
	"time"
)

// offlineRT fails every provider request instantly, so tests exercise
// cache semantics without touching real provider endpoints.
type offlineRT struct{}

func (offlineRT) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("offline")
}

func newTestService() *Service {
	svc := NewService()
	svc.client = &http.Client{Transport: offlineRT{}}
	// The Antigravity fetcher shells out to the agy CLI, which works
	// on this box — stub it so tests stay offline and instant.
	stub := svc.fetchers
	for i := range stub {
		if stub[i].idx == 3 {
			stub[i].fn = func(*http.Client) ProviderQuota {
				return ProviderQuota{ID: "antigravity", Name: "Antigravity"}
			}
		}
	}
	svc.fetchers = stub
	return svc
}

func TestQuotaTTLIsFiveMinutes(t *testing.T) {
	if quotaTTL != 5*time.Minute {
		t.Fatalf("quotaTTL = %s, want 5m (provider-request cap)", quotaTTL)
	}
}

func TestServiceServesFreshCacheRegardlessOfForce(t *testing.T) {
	svc := newTestService()
	snap1 := svc.Get()
	snap2 := svc.Get()
	if snap1.UpdatedAt != snap2.UpdatedAt {
		t.Fatal("fresh cache was refetched inside the TTL")
	}
}

func TestServiceStaleServesSnapshotAndRevalidates(t *testing.T) {
	svc := newTestService()
	snap1 := svc.Get()

	svc.mu.Lock()
	svc.cachedAt = time.Now().Add(-6 * time.Minute)
	svc.mu.Unlock()

	stale := svc.Get()
	if stale.UpdatedAt != snap1.UpdatedAt {
		t.Fatal("stale serve should return the cached snapshot instantly")
	}
	if !stale.Refreshing {
		t.Fatal("stale serve must set Refreshing so the client can hint")
	}

	// The background revalidate completes fast on the offline
	// transport; a follow-up Get must see fresh data without it.
	deadline := time.Now().Add(3 * time.Second)
	for {
		next := svc.Get()
		if !next.Refreshing && next.UpdatedAt != snap1.UpdatedAt {
			return
		}
		if time.Now().After(deadline) {
			t.Fatal("background revalidate never landed a fresh snapshot")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

func TestServiceStaleSingleFlight(t *testing.T) {
	svc := newTestService()
	svc.Get()

	svc.mu.Lock()
	svc.cachedAt = time.Now().Add(-6 * time.Minute)
	svc.mu.Unlock()

	svc.Get()
	svc.Get()
	if svc.pending == false {
		t.Fatal("pending flag should hold across concurrent stale hits")
	}
	svc.mu.Lock()
	svc.pending = false // let the test exit without waiting on the goroutine
	svc.mu.Unlock()
}
