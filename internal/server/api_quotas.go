package server

import (
	"net/http"

	"github.com/EstebanForge/couch-pi/internal/quotas"
)

// Both quota endpoints serve the cached snapshot: the quotas service's
// TTL is the provider-protection cap, so a refresh request inside the
// window answers the cache unchanged and a stale one revalidates in
// the background with Refreshing set on the wire.
func (a *app) apiQuotas(w http.ResponseWriter, r *http.Request) {
	if a.quotas == nil {
		a.quotas = quotas.NewService()
	}
	writeJSON(w, http.StatusOK, a.quotas.Get())
}
