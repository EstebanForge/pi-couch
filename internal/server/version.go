package server

import "net/http"

// Couch Pi's own semver. The phone reads it from /api/version to show
// which build the server runs; bump it on every deploy-worthy change.
const Version = "0.1.0+beta24"

func (a *app) apiVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"version": Version})
}
