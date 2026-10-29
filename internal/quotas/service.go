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
	pending  bool          // one background revalidate at a time
	ready    chan struct{} // closed once the first cold fetch lands
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
// revalidate runs; a cold service fetches once, outside the lock, and
// concurrent cold callers wait on that same fetch instead of pinning
// the mutex through a full provider round.
func (s *Service) Get() QuotasSnapshot {
	s.mu.Lock()
	if s.cached != nil && time.Since(s.cachedAt) < quotaTTL {
		out := *s.cached
		s.mu.Unlock()
		return out
	}
	if s.cached != nil {
		if !s.pending {
			s.pending = true
			go s.revalidate()
		}
		out := *s.cached
		out.Refreshing = true
		s.mu.Unlock()
		return out
	}
	if s.ready == nil {
		s.ready = make(chan struct{})
		s.mu.Unlock()
		snap := s.fetch()
		s.mu.Lock()
		s.cached = &snap
		s.cachedAt = time.Now()
		close(s.ready)
		s.mu.Unlock()
		return snap
	}
	ready := s.ready
	s.mu.Unlock()
	<-ready
	return s.Get()
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
