// Package store persists per-session sent comments as one JSON file each.
// Pending comments live on the device; only submitted ones reach the server.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// Comment is one submitted line comment.
type Comment struct {
	TS   int64  `json:"ts"`
	File string `json:"file"`
	Line int    `json:"line"`
	Body string `json:"body"`
}

// ErrBadKey rejects session keys that could escape the state dir.
var ErrBadKey = errors.New("bad session key")

// Store is safe for concurrent use.
type Store struct {
	dir string
	mu  sync.Mutex
}

// New returns a store rooted at the XDG state dir.
func New() (*Store, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "couch-pi")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// NewDir returns a store under an explicit dir (tests).
func NewDir(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// Sent lists submitted comments for a session; none means empty, not error.
func (s *Store) Sent(key string) ([]Comment, error) {
	if err := checkKey(key); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.read(key)
}

// Append adds submitted comments to a session's state file.
func (s *Store) Append(key string, cs []Comment) error {
	if err := checkKey(key); err != nil {
		return err
	}
	if len(cs) == 0 {
		return nil
	}
	now := time.Now().Unix()
	s.mu.Lock()
	defer s.mu.Unlock()
	all, err := s.read(key)
	if err != nil {
		return err
	}
	for i := range cs {
		if cs[i].TS == 0 {
			cs[i].TS = now
		}
	}
	all = append(all, cs...)
	b, err := json.Marshal(all)
	if err != nil {
		return err
	}
	return os.WriteFile(s.path(key), b, 0o644)
}

func (s *Store) read(key string) ([]Comment, error) {
	b, err := os.ReadFile(s.path(key))
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var cs []Comment
	if err := json.Unmarshal(b, &cs); err != nil {
		return nil, fmt.Errorf("state %s: %w", key, err)
	}
	return cs, nil
}

func (s *Store) path(key string) string {
	return filepath.Join(s.dir, key+".json")
}

func checkKey(key string) error {
	if key == "" || strings.ContainsAny(key, "/\\") || strings.Contains(key, "..") {
		return ErrBadKey
	}
	return nil
}
