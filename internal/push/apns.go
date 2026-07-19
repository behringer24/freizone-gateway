package push

import (
	"context"
	"errors"
)

// ErrAPNSNotImplemented is returned by apnsSender.Send. The config
// surface (GATEWAY_APNS_* env vars, config.Config.APNSConfigured) is
// already in place -- see internal/config -- so wiring in a real sender
// (e.g. via github.com/sideshow/apns2) later needs no architectural
// change here, just an implementation.
var ErrAPNSNotImplemented = errors.New("push: apns sending is not implemented yet")

type apnsSender struct{}

// NewAPNSSender returns a Sender that always reports
// ErrAPNSNotImplemented. Kept as a real Sender (rather than omitting
// apns from the sender map entirely) so callers get one consistent
// "not configured / not implemented" error path regardless of platform.
func NewAPNSSender() Sender {
	return &apnsSender{}
}

func (s *apnsSender) Send(ctx context.Context, target Target) error {
	return ErrAPNSNotImplemented
}
