// Package push sends VAPID web push notifications on herdr Status
// transitions (P5) and watches those transitions through herdr agent.wait
// (ADR-0005). Transitions notify; stationary states never do.
package push

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/EstebanForge/couch-pi/internal/store"
	"github.com/SherClockHolmes/webpush-go"
)

// Message is the payload delivered to service workers. URL is the
// deep-link the client opens on notificationclick.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url"`
}

// Sender pushes messages to browser subscriptions. Safe for concurrent use.
type Sender struct {
	Public     string
	Private    string
	Subscriber string // VAPID sub claim, e.g. mailto: operator address
	TTL        int
	Timeout    time.Duration
}

// Send delivers the message to every subscription. Subscriptions whose
// endpoint answered 410 Gone are returned as dead so the caller can drop
// them (spec: revoked subscriptions self-clean). Other endpoint failures
// surface as one joined error; the message still went to any healthy
// subscriptions.
func (s *Sender) Send(ctx context.Context, subs []store.Sub, m Message) (dead []string, err error) {
	client := &http.Client{Timeout: s.Timeout}
	var errs []error
	for _, sub := range subs {
		payload, err := json.Marshal(m)
		if err != nil {
			return dead, err
		}
		resp, err := webpush.SendNotificationWithContext(ctx,
			payload,
			&webpush.Subscription{
				Endpoint: sub.Endpoint,
				Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
			},
			&webpush.Options{
				HTTPClient:      client,
				Subscriber:      s.Subscriber,
				VAPIDPublicKey:  s.Public,
				VAPIDPrivateKey: s.Private,
				TTL:             s.TTL,
				Urgency:         webpush.UrgencyNormal,
			})
		if err != nil {
			errs = append(errs, fmt.Errorf("push %s: %w", sub.Endpoint, err))
			continue
		}
		if resp.StatusCode == http.StatusGone || resp.StatusCode == http.StatusNotFound {
			// Spec: a revoked or reaped endpoint answers 410 (or 404) and
			// the subscription is permanently dead; keep sending to it and
			// every push retries a corpse.
			dead = append(dead, sub.Endpoint)
		} else if resp.StatusCode >= 300 {
			errs = append(errs, fmt.Errorf("push %s: endpoint answered %d", sub.Endpoint, resp.StatusCode))
		}
		_ = resp.Body.Close()
	}
	if len(errs) > 0 {
		return dead, errs[0]
	}
	return dead, nil
}
