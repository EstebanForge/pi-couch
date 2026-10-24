package server

import (
	"cmp"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Health surface of the agent memory server, classified the way the pi
// agentmemory extension reads it: degraded and critical are operational
// states (server up, reads and writes work), and a gated or 503ing
// health path falls back to unauthenticated liveness rather than
// reading as down.

const (
	memoryTTL        = 10 * time.Second
	memoryProbeWait  = 5 * time.Second
	memoryColdWait   = 700 * time.Millisecond
	memoryDefaultURL = "http://localhost:3111"
)

type memoryProbe struct {
	mu      sync.Mutex
	secret  string
	url     string
	state   string
	at      time.Time
	pending atomic.Bool
}

func newMemoryProbe(cfgURL, cfgSecret string) *memoryProbe {
	m := newMemoryProbeAt(cmp.Or(os.Getenv("AGENTMEMORY_URL"), cfgURL, memoryDefaultURL))
	m.secret = cmp.Or(os.Getenv("AGENTMEMORY_SECRET"), cfgSecret)
	return m
}

func newMemoryProbeAt(url string) *memoryProbe {
	return &memoryProbe{url: strings.TrimRight(url, "/")}
}

// classCold serves the cached state, probing synchronously with a
// short deadline when nothing has been fetched yet, so the first
// reset frame carries a real class instead of an empty slot.
// class serves the cached state. A never-probed cache probes
// synchronously with the short cold deadline so the first reset frame
// carries a real class; later refreshes run in the background so
// stream ticks never block on a dead route.
func (m *memoryProbe) class(_ context.Context) string {
	m.mu.Lock()
	primed := !m.at.IsZero()
	fresh := time.Since(m.at) < memoryTTL
	st := m.state
	m.mu.Unlock()
	if primed && fresh {
		return st
	}
	if !m.pending.CompareAndSwap(false, true) {
		return st
	}
	if !primed {
		defer m.pending.Store(false)
		st := m.probe(memoryColdWait)
		m.mu.Lock()
		m.state = st
		m.at = time.Now()
		m.mu.Unlock()
		return st
	}
	go func() {
		defer m.pending.Store(false)
		m.fetch()
	}()
	return st
}

// seen marks the cache fresh without probing, for tests.
func (m *memoryProbe) seen() {
	m.mu.Lock()
	m.at = time.Now()
	m.mu.Unlock()
}

func (m *memoryProbe) fetch() string {
	st := m.probe(memoryProbeWait)
	m.mu.Lock()
	m.state = st
	m.at = time.Now()
	m.mu.Unlock()
	return st
}

func (m *memoryProbe) probe(timeout time.Duration) string {
	client := &http.Client{Timeout: timeout}
	mk := func(url string) (*http.Request, error) {
		r, err := http.NewRequest(http.MethodGet, url, nil)
		if err == nil && m.secret != "" {
			r.Header.Set("Authorization", "Bearer "+m.secret)
		}
		return r, err
	}
	req, err := mk(m.url + "/agentmemory/health")
	if err == nil {
		res, derr := client.Do(req)
		if derr == nil {
			body, rerr := io.ReadAll(io.LimitReader(res.Body, 64<<10))
			res.Body.Close()
			if rerr == nil && res.StatusCode == http.StatusOK {
				return classifyMemory(body)
			}
		}
	}
	lreq, lerr := mk(m.url + "/agentmemory/livez")
	if lerr != nil {
		return "down"
	}
	lres, err := client.Do(lreq)
	if err != nil {
		return "down"
	}
	lres.Body.Close()
	if lres.StatusCode < 300 {
		return "unknown"
	}
	return "down"
}

func classifyMemory(body []byte) string {
	var r struct {
		Status string `json:"status"`
		Health *struct {
			Status string `json:"status"`
		} `json:"health"`
	}
	if json.Unmarshal(body, &r) != nil {
		return "unknown"
	}
	s := r.Status
	if s == "" && r.Health != nil {
		s = r.Health.Status
	}
	switch s {
	case "healthy":
		return "healthy"
	case "degraded", "critical":
		return "degraded"
	case "unhealthy":
		return "unhealthy"
	case "down":
		return "down"
	default:
		return "unknown"
	}
}
