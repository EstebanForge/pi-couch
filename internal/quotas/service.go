package quotas

import (
	"net/http"
	"sync"
	"time"
)

// Service coordinates cached fetches across all quota providers.
type Service struct {
	client   *http.Client
	mu       sync.RWMutex
	cached   *QuotasSnapshot
	cachedAt time.Time
	ttl      time.Duration
}

// NewService returns a new Quotas service with a 60s cache TTL.
func NewService() *Service {
	return &Service{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		ttl: 60 * time.Second,
	}
}

// Get returns the snapshot, using the cache if fresh.
func (s *Service) Get(force bool) QuotasSnapshot {
	s.mu.RLock()
	if !force && s.cached != nil && time.Since(s.cachedAt) < s.ttl {
		defer s.mu.RUnlock()
		return *s.cached
	}
	s.mu.RUnlock()

	return s.Refresh()
}

// Refresh triggers parallel fetches across all supported providers.
func (s *Service) Refresh() QuotasSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()

	var wg sync.WaitGroup
	providers := make([]ProviderQuota, 6)

	fetchers := []struct {
		idx int
		fn  func(*http.Client) ProviderQuota
	}{
		{0, fetchClaudeQuota},
		{1, fetchCodexQuota},
		{2, fetchCopilotQuota},
		{3, fetchAntigravityQuota},
		{4, fetchZaiQuota},
		{5, fetchMinimaxQuota},
	}

	wg.Add(len(fetchers))
	for _, f := range fetchers {
		go func(idx int, fn func(*http.Client) ProviderQuota) {
			defer wg.Done()
			providers[idx] = fn(s.client)
		}(f.idx, f.fn)
	}

	wg.Wait()

	snap := QuotasSnapshot{
		Providers: providers,
		UpdatedAt: time.Now(),
	}
	s.cached = &snap
	s.cachedAt = time.Now()
	return snap
}
