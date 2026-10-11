// Subscriptions and VAPID keys for web push, persisted in the state dir.
package store

import (
	"crypto/ecdh"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
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

// SetSubs replaces all push subscriptions (subscribe and 410 cleanup both
// rewrite the list).
func (s *Store) SetSubs(subs []Sub) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := json.Marshal(subs)
	if err != nil {
		return err
	}
	return os.WriteFile(s.subsPath(), b, 0o644)
}

// VAPID returns the push identity, generating and persisting it on first
// use. Keys are base64url in the web-push convention: the 65-byte
// uncompressed P-256 public point and the 32-byte private scalar, the exact
// formats webpush-go and the browser applicationServerKey both expect.
// The private key file is 0600.
func (s *Store) VAPID() (VAPID, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	b, err := os.ReadFile(s.vapidPath())
	if err == nil {
		var v VAPID
		if err := json.Unmarshal(b, &v); err == nil && v.Public != "" && v.Private != "" {
			return v, nil
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return VAPID{}, err
	}
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
	b, err = json.Marshal(v)
	if err != nil {
		return VAPID{}, err
	}
	if err := os.WriteFile(s.vapidPath(), b, 0o600); err != nil {
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
