package api

import "net/http"

// handleGetCapabilities is a public discovery endpoint: which platforms
// this gateway instance can actually deliver to. Lets an operator verify
// their own config at a glance, and lets a caller (or, eventually, an
// app) know what to expect before calling POST /v1/push/send.
func (a *API) handleGetCapabilities(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, a.Capabilities)
}
