// Package api implements the gateway's HTTP surface: health/capability
// discovery and the one authenticated push-send endpoint.
package api

import (
	"log/slog"
	"net/http"

	"github.com/behringer24/freizone-gateway/internal/auth"
	"github.com/behringer24/freizone-gateway/internal/push"
)

// API holds the shared dependencies used by all handlers.
type API struct {
	Auth   *auth.Middleware
	Logger *slog.Logger

	// Senders maps a push.Platform* constant to the Sender that handles
	// it. Always fully populated (apns included, backed by a stub that
	// errors) so handleSendPush has one consistent dispatch path;
	// Capabilities is the separate, explicit source of truth for what's
	// actually usable.
	Senders map[string]push.Sender

	// Capabilities maps a push.Platform* constant to whether this
	// instance is actually configured to send on it -- what
	// handleGetCapabilities reports, and what handleSendPush checks
	// before dispatching (a 501, not a dispatch-and-fail, for an
	// unconfigured platform).
	Capabilities map[string]bool
}

// New builds an API with the given dependencies.
func New(authMW *auth.Middleware, senders map[string]push.Sender, capabilities map[string]bool, logger *slog.Logger) *API {
	return &API{Auth: authMW, Senders: senders, Capabilities: capabilities, Logger: logger}
}

// Router builds the full HTTP route table.
func (a *API) Router() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", a.handleHealth)
	mux.HandleFunc("GET /v1/capabilities", a.handleGetCapabilities)
	mux.Handle("POST /v1/push/send", a.Auth.Require(http.HandlerFunc(a.handleSendPush)))

	return mux
}
