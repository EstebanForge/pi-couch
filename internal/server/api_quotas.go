package server

import (
	"net/http"

	"github.com/EstebanForge/couch-pi/internal/quotas"
)

func (a *app) apiQuotas(w http.ResponseWriter, r *http.Request) {
	if a.quotas == nil {
		a.quotas = quotas.NewService()
	}
	snap := a.quotas.Get(false)
	writeJSON(w, http.StatusOK, snap)
}

func (a *app) apiQuotasRefresh(w http.ResponseWriter, r *http.Request) {
	if a.quotas == nil {
		a.quotas = quotas.NewService()
	}
	snap := a.quotas.Get(true)
	writeJSON(w, http.StatusOK, snap)
}
