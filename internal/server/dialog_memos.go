package server

import (
	"sync"
	"time"
)

// dialogClaimTTL bounds a claim's life: the memo's job is only the
// in-flight double-tap race (dispatch ctx is 20s and herdr keeps
// delivering queued keys after the handler returns), while pi
// legitimately re-presents identical dialog content minutes later:
// same pane id, new instance (2026-10-28 stuck-commit incident).
const dialogClaimTTL = 60 * time.Second

// dialogMemos dedupes dialog answers. A claim is recorded before any
// key goes out, so a second tap on the same dialog card is refused
// instead of firing a second Enter into whatever pi shows next (the
// herdr socket has no answer receipt, and the bridge end event races
// the taps). This is the pi-durable memo pattern: first writer wins,
// the claim releases when the send provably did not happen, and it
// expires after dialogClaimTTL so a re-presented dialog answers as
// the new instance it is.
type dialogMemos struct {
	mu   sync.Mutex
	cur  map[string]time.Time
	prev map[string]time.Time
	cap  int
	now  func() time.Time
}

func newDialogMemos() *dialogMemos {
	return &dialogMemos{cur: map[string]time.Time{}, prev: map[string]time.Time{}, cap: 512, now: time.Now}
}

// Claim records id as answered. False means an unexpired claim for
// this id already exists.
func (m *dialogMemos) Claim(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := m.now()
	for _, gen := range []map[string]time.Time{m.cur, m.prev} {
		if ts, dup := gen[id]; dup && now.Sub(ts) < dialogClaimTTL {
			return false
		}
	}
	if len(m.cur) >= m.cap {
		m.prev = m.cur
		m.cur = map[string]time.Time{}
	}
	m.cur[id] = now
	return true
}

// Release drops a claim whose keys never went out, so a corrected
// retry can proceed. Both generations: rotation would otherwise make
// a claim unreleasable.
func (m *dialogMemos) Release(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cur, id)
	delete(m.prev, id)
}
