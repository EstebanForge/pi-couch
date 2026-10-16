package server

import "sync"

// dialogMemos dedupes dialog answers. A claim is recorded before any
// key goes out, so a second tap on the same dialog card is refused
// instead of firing a second Enter into whatever pi shows next (the
// herdr socket has no answer receipt, and the bridge end event races
// the taps). This is the pi-durable memo pattern: first writer wins,
// the claim releases when the send provably did not happen.
type dialogMemos struct {
	mu   sync.Mutex
	cur  map[string]struct{}
	prev map[string]struct{}
	cap  int
}

func newDialogMemos() *dialogMemos {
	return &dialogMemos{cur: map[string]struct{}{}, prev: map[string]struct{}{}, cap: 512}
}

// Claim records id as answered. False means an answer for this id was
// already claimed and not released.
func (m *dialogMemos) Claim(id string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, dup := m.cur[id]; dup {
		return false
	}
	if _, dup := m.prev[id]; dup {
		return false
	}
	if len(m.cur) >= m.cap {
		m.prev = m.cur
		m.cur = map[string]struct{}{}
	}
	m.cur[id] = struct{}{}
	return true
}

// Release drops a claim whose keys never went out, so a corrected
// retry can proceed.
func (m *dialogMemos) Release(id string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.cur, id)
}
