// Subscriptions and VAPID keys for web push, persisted in the state dir.
package store

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Sub is one browser push subscription (PushManager.subscribe output).
type Sub struct {
	Endpoint string `json:"endpoint"`
	P256dh   string `json:"p256dh"`
	Auth     string `json:"auth"`
}

// VAPID holds the server's push identity keypair, base64url raw keys.
type VAPID struct {
	Public  string `json:"public"`
	Private string `json:"private"`
}

// Subs lists stored push subscriptions.
func (s *Store) Subs() ([]Sub, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readSubs()
}

// UpdateSubs applies fn to the stored list under one lock, so read-modify-write
// callers (subscribe, unsubscribe, 410 pruning) never lose concurrent updates.
func (s *Store) UpdateSubs(fn func([]Sub) []Sub) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	subs, err := s.readSubs()
	if err != nil {
		return err
	}
	b, err := json.Marshal(fn(subs))
	if err != nil {
		return err
	}
	return WriteFileAtomic(s.subsPath(), b, 0o600)
}

// SetSubs replaces the whole list.
func (s *Store) SetSubs(subs []Sub) error {
	return s.UpdateSubs(func([]Sub) []Sub { return subs })
}

// VAPID returns the push identity, generating and persisting it on first
// use. Keys are base64url in the web-push convention: the 65-byte
// uncompressed P-256 public point and the 32-byte private scalar, the exact
// formats webpush-go and the browser applicationServerKey both expect.
// The private key file is 0600. A corrupt key file is an error, never a
// silent regeneration: new keys would invalidate every existing subscription.
func (s *Store) VAPID() (VAPID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.vapidPath())
	if errors.Is(err, os.ErrNotExist) {
		return s.generateVAPID()
	}
	if err != nil {
		return VAPID{}, err
	}
	var v VAPID
	if err := json.Unmarshal(b, &v); err != nil || v.Public == "" || v.Private == "" {
		return VAPID{}, fmt.Errorf("corrupt %s: fix or remove it by hand; keys must not regenerate", s.vapidPath())
	}
	return v, nil
}

func (s *Store) generateVAPID() (VAPID, error) {
	d := make([]byte, 32)
	if _, err := rand.Read(d); err != nil {
		return VAPID{}, err
	}
	priv, err := ecdh.P256().NewPrivateKey(d)
	if err != nil {
		return VAPID{}, err
	}
	v := VAPID{
		Public:  base64.RawURLEncoding.EncodeToString(priv.PublicKey().Bytes()),
		Private: base64.RawURLEncoding.EncodeToString(d),
	}
	b, err := json.Marshal(v)
	if err != nil {
		return VAPID{}, err
	}
	if err := WriteFileAtomic(s.vapidPath(), b, 0o600); err != nil {
		return VAPID{}, err
	}
	return v, nil
}

func (s *Store) readSubs() ([]Sub, error) {
	b, err := os.ReadFile(s.subsPath())
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var subs []Sub
	if err := json.Unmarshal(b, &subs); err != nil {
		return nil, err
	}
	return subs, nil
}

func (s *Store) subsPath() string { return filepath.Join(s.dir, "push-subs.json") }
func (s *Store) vapidPath() string {
	return filepath.Join(s.dir, "vapid.json")
}
