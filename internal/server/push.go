// Push subscription HTTP surface: the browser needs the VAPID public key,
// subscribes, and unsubscribes. Plain JSON POSTs per the native door rule.
package server

import (
	"encoding/json"
	"net/http"

	"github.com/EstebanForge/couch-pi/internal/store"
	"github.com/EstebanForge/couch-pi/internal/web"
)

func (a *app) pushKey(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"publicKey": a.vapidPublic})
}

func (a *app) pushSubscribe(w http.ResponseWriter, r *http.Request) {
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
	subs, err := a.st.Subs()
	if err != nil {
		http.Error(w, "state", http.StatusInternalServerError)
		return
	}
	out := append(filterEndpoint(subs, sub.Endpoint), store.Sub{
		Endpoint: sub.Endpoint,
		P256dh:   sub.Keys.P256dh,
		Auth:     sub.Keys.Auth,
	})
	if err := a.st.SetSubs(out); err != nil {
		http.Error(w, "state", http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (a *app) pushUnsubscribe(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Endpoint string `json:"endpoint"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Endpoint == "" {
		http.Error(w, "bad endpoint", http.StatusBadRequest)
		return
	}
	subs, err := a.st.Subs()
	if err != nil {
		http.Error(w, "state", http.StatusInternalServerError)
		return
	}
	if err := a.st.SetSubs(filterEndpoint(subs, body.Endpoint)); err != nil {
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

// serviceWorker serves /sw.js from the embed; scope "/" covers all routes.
func (a *app) serviceWorker(w http.ResponseWriter, r *http.Request) {
	b, err := web.ServiceWorker()
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	_, _ = w.Write(b)
}
