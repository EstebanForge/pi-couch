package quotas

import (
	"net/http"
	"sync"
	"time"
)

// quotaTTL caps how often providers are contacted: at most one fetch
// round per window regardless of who asks (scheduled, screen open or
// the refresh button), so the phone can never hammer provider APIs.
const quotaTTL = 5 * time.Minute

// Service coordinates cached fetches across all quota providers.
type Service struct {
	client   *http.Client
	fetchers []struct {
		idx int
		fn  func(*http.Client) ProviderQuota
	}
	mu       sync.Mutex
	cached   *QuotasSnapshot
	cachedAt time.Time
	pending  bool // one background revalidate at a time
}

func defaultFetchers() []struct {
	idx int
	fn  func(*http.Client) ProviderQuota
} {
	return []struct {
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
}

func NewService() *Service {
	return &Service{
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
		fetchers: defaultFetchers(),
	}
}

// Get returns the snapshot. A fresh cache answers as-is; a stale one
// answers instantly with Refreshing set while a single background
// revalidate runs; only a cold service fetches inline, because there
// is nothing older to show.
func (s *Service) Get() QuotasSnapshot {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.cached != nil {
		if time.Since(s.cachedAt) < quotaTTL {
			return *s.cached
		}
		if !s.pending {
			s.pending = true
			go s.revalidate()
		}
		out := *s.cached
		out.Refreshing = true
		return out
	}
	return s.refreshLocked()
}

func (s *Service) refreshLocked() QuotasSnapshot {
	snap := s.fetch()
	s.cached = &snap
	s.cachedAt = time.Now()
	return snap
}

// revalidate swaps in a fresh snapshot off-request; the pending flag
// collapses concurrent stale hits into one provider round.
func (s *Service) revalidate() {
	snap := s.fetch()
	s.mu.Lock()
	s.cached = &snap
	s.cachedAt = time.Now()
	s.pending = false
	s.mu.Unlock()
}

// fetch runs all provider fetchers in parallel. Callers hold no lock:
// provider round trips must never block a Get.
func (s *Service) fetch() QuotasSnapshot {
	var wg sync.WaitGroup
	providers := make([]ProviderQuota, 6)

	fetchers := s.fetchers
	if fetchers == nil {
		fetchers = defaultFetchers()
	}

	wg.Add(len(fetchers))
	for _, f := range fetchers {
		go func(idx int, fn func(*http.Client) ProviderQuota) {
			defer wg.Done()
			providers[idx] = fn(s.client)
		}(f.idx, f.fn)
	}

	wg.Wait()

	return QuotasSnapshot{
		Providers: providers,
		UpdatedAt: time.Now(),
	}
}
