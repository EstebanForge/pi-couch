package server

import "net/http"

// Pi Couch's own version. The phone reads it from /api/version to show
// which build the server runs; bump it on every deploy-worthy change.
const Version = "20261030"

func (a *app) apiVersion(w http.ResponseWriter, _ *http.Request) {
	driver := "herdr"
	if a.hcDrv != nil {
		driver = "hc"
	}
	writeJSON(w, http.StatusOK, map[string]string{"version": Version, "home": a.home, "driver": driver})
}
