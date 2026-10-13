// Push subscription HTTP surface: the browser needs the VAPID public key,
// subscribes, and unsubscribes. Plain JSON POSTs per the native door rule.
package server

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"

	"github.com/EstebanForge/couch-pi/internal/store"
)

// pushServiceHosts is the allowlist of push services the server relays
// VAPID-signed requests to. The endpoint is client-supplied, so without an
// allowlist the server is an open signed-POST relay.
var pushServiceHosts = []string{
	"fcm.googleapis.com",
	"push.services.mozilla.com",
	"push.apple.com",
	"notify.windows.com",
}

// maxSubs bounds the stored subscription list; the oldest entries rotate out.
const maxSubs = 16

// endpointAllowed accepts https push-service endpoints, plus http on the
// loopback for the local test receiver and development.
func endpointAllowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return false
	}
	host := u.Hostname()
	loopback := host == "127.0.0.1" || host == "::1" || host == "localhost"
	switch u.Scheme {
	case "https":
	case "http":
		if !loopback {
			return false
		}
	default:
		return false
	}
	if loopback {
		return true
	}
	for _, svc := range pushServiceHosts {
		if host == svc || strings.HasSuffix(host, "."+svc) {
			return true
		}
	}
	return false
}

func (a *app) pushKey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": a.vapidPublic})
}

func (a *app) pushSubscribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var sub struct {
		Endpoint string `json:"endpoint"`
		Keys     struct {
			P256dh string `json:"p256dh"`
			Auth   string `json:"auth"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(r.Body).Decode(&sub); err != nil || sub.Endpoint == "" || sub.Keys.P256dh == "" || sub.Keys.Auth == "" {
		http.Error(w, "bad subscription", http.StatusBadRequest)
		return
	}
	if !endpointAllowed(sub.Endpoint) {
		http.Error(w, "push endpoint not allowed", http.StatusBadRequest)
		return
	}
	err := a.st.UpdateSubs(func(subs []store.Sub) []store.Sub {
		out := append(filterEndpoint(subs, sub.Endpoint), store.Sub{
			Endpoint: sub.Endpoint,
			P256dh:   sub.Keys.P256dh,
			Auth:     sub.Keys.Auth,
		})
		if len(out) > maxSubs {
			out = out[len(out)-maxSubs:] // rotate the oldest out
		}
		return out
	})
	if err != nil {
		http.Error(w, "state", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 64<<10)
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Endpoint == "" {
		http.Error(w, "bad endpoint", http.StatusBadRequest)
		return
	}
	if err := a.st.UpdateSubs(func(subs []store.Sub) []store.Sub {
		return filterEndpoint(subs, body.Endpoint)
	}); err != nil {
		http.Error(w, "state", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func filterEndpoint(subs []store.Sub, endpoint string) []store.Sub {
	out := subs[:0]
	for _, s := range subs {
		if s.Endpoint != endpoint {
			out = append(out, s)
		}
	}
	return out
}
