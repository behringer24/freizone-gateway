package api

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/behringer24/freizone-gateway/internal/auth"
	"github.com/behringer24/freizone-gateway/internal/push"
)

// sendTimeout bounds how long a single push-send call is allowed to
// take, so a slow upstream (FCM/APNs) can never pile up handler
// goroutines -- the caller (freizone-server) already treats this as a
// best-effort, fire-and-forget call on its side.
const sendTimeout = 10 * time.Second

// handleSendPush delivers one content-free wake to the given platform
// target. Authentication (internal/auth.Middleware) has already
// verified the caller's signature and that its key isn't revoked by the
// time this runs -- no per-caller registration or rate limiting beyond
// that exists today.
func (a *API) handleSendPush(w http.ResponseWriter, r *http.Request) {
	var req sendPushRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}
	if req.Token == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "token is required")
		return
	}
	if req.Platform != push.PlatformFCM && req.Platform != push.PlatformAPNS {
		writeError(w, http.StatusBadRequest, "unknown_platform", "platform must be one of: fcm, apns")
		return
	}

	// Checked against Capabilities, not Senders map membership: a
	// recognized-but-unconfigured platform (e.g. fcm with no
	// GATEWAY_FCM_CREDENTIALS_FILE set) has no entry in Senders at all,
	// and must be reported as "not configured" (501), not "unknown" (400).
	if !a.Capabilities[req.Platform] {
		writeError(w, http.StatusNotImplemented, "platform_not_configured", "this gateway instance is not configured for "+req.Platform)
		return
	}

	sender, ok := a.Senders[req.Platform]
	if !ok {
		// Capabilities said this platform is configured, but no Sender
		// was wired up for it -- a wiring bug in cmd/gateway, not a bad
		// request.
		writeError(w, http.StatusInternalServerError, "internal", "internal server error")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), sendTimeout)
	defer cancel()

	if err := sender.Send(ctx, push.Target{Platform: req.Platform, Token: req.Token}); err != nil {
		if a.Logger != nil {
			caller, _ := auth.CallerFromContext(r.Context())
			a.Logger.Warn("push send failed", "error", err, "platform", req.Platform, "caller", caller)
		}
		writeError(w, http.StatusBadGateway, "send_failed", "upstream push service call failed")
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "sent"})
}
