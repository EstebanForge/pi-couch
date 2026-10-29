package quotas

import (
	"errors"
	"net/http"
	"sync/atomic"
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

// Concurrent cold callers share the first fetch: the provider round
// runs once, everyone gets the same snapshot, and nothing deadlocks.
func TestServiceColdSingleFlight(t *testing.T) {
	svc := newTestService()
	var rounds atomic.Int32
	stub := svc.fetchers
	for i := range stub {
		fn := stub[i].fn
		stub[i].fn = func(c *http.Client) ProviderQuota {
			rounds.Add(1)
			time.Sleep(50 * time.Millisecond)
			return fn(c)
		}
	}
	svc.fetchers = stub

	const n = 8
	start := make(chan struct{})
	results := make(chan QuotasSnapshot, n)
	for i := 0; i < n; i++ {
		go func() {
			<-start
			results <- svc.Get()
		}()
	}
	close(start)

	var first QuotasSnapshot
	for i := 0; i < n; i++ {
		select {
		case snap := <-results:
			if i == 0 {
				first = snap
			} else if snap.UpdatedAt != first.UpdatedAt {
				t.Fatal("cold callers got different snapshots")
			}
		case <-time.After(5 * time.Second):
			t.Fatal("cold Get deadlocked")
		}
	}
	if got := rounds.Load(); got != int32(len(stub)) {
		t.Fatalf("provider fns ran %d times, want %d (one round)", got, len(stub))
	}
}
