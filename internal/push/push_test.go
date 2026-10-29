package push

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/EstebanForge/couch-pi/internal/store"
	"github.com/SherClockHolmes/webpush-go"
)

// sub builds a subscription whose p256dh is a real P-256 point; the
// encryption path validates it even against test endpoints.
func sub(t *testing.T, endpoint string) store.Sub {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	_ = priv
	auth := make([]byte, 16)
	if _, err := rand.Read(auth); err != nil {
		t.Fatal(err)
	}
	return store.Sub{
		Endpoint: endpoint,
		P256dh:   pub,
		Auth:     base64.RawURLEncoding.EncodeToString(auth),
	}
}

func testSender(t *testing.T) *Sender {
	t.Helper()
	priv, pub, err := webpush.GenerateVAPIDKeys()
	if err != nil {
		t.Fatal(err)
	}
	return &Sender{
		Public:     pub,
		Private:    priv,
		Subscriber: "mailto:couch-pi@test",
		TTL:        3600,
		Timeout:    5 * time.Second,
	}
}

func TestSendDeliversPayload(t *testing.T) {
	var gotLen int
	var gotEncoding string
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		gotLen = len(b)
		gotEncoding = r.Header.Get("Content-Encoding")
		gotAuth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusCreated)
	}))
	defer srv.Close()

	s := testSender(t)
	dead, err := s.Send(context.Background(),
		[]store.Sub{sub(t, srv.URL)},
		Message{Title: "Couch Pi: probe", Body: "Agent completed task execution.", URL: "/sessions/k"})
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 0 {
		t.Fatalf("dead: %v", dead)
	}
	// The endpoint sees RFC 8291 ciphertext, not the plaintext payload.
	if gotLen == 0 || gotEncoding != "aes128gcm" {
		t.Fatalf("body %d bytes, encoding %q", gotLen, gotEncoding)
	}
	if gotAuth == "" {
		t.Fatal("missing VAPID Authorization header")
	}
}

func TestSendDrops410(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer srv.Close()

	s := testSender(t)
	dead, err := s.Send(context.Background(),
		[]store.Sub{sub(t, srv.URL)},
		Message{})
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 1 || dead[0] != srv.URL {
		t.Fatalf("dead: %v", dead)
	}
}

// A 404 endpoint is as dead as a 410: the subscription was reaped
// server-side and retrying only burns the next notification.
func TestSendDrops404(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	s := testSender(t)
	dead, err := s.Send(context.Background(),
		[]store.Sub{sub(t, srv.URL)},
		Message{})
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 1 || dead[0] != srv.URL {
		t.Fatalf("dead: %v", dead)
	}
}

func TestSendAliveAndDeadMixed(t *testing.T) {
	gone := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusGone)
	}))
	defer gone.Close()
	live := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
	}))
	defer live.Close()

	s := testSender(t)
	dead, err := s.Send(context.Background(),
		[]store.Sub{sub(t, gone.URL), sub(t, live.URL)},
		Message{})
	if err != nil {
		t.Fatal(err)
	}
	if len(dead) != 1 || dead[0] != gone.URL {
		t.Fatalf("dead: %v", dead)
	}
}

func TestSendServerErrorMessageNotDead(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()

	s := testSender(t)
	_, err := s.Send(context.Background(),
		[]store.Sub{sub(t, srv.URL)},
		Message{})
	if err == nil {
		t.Fatal("want error for 500")
	}
}
