// Package push abstracts sending a wake notification to a device,
// regardless of platform. Every Sender sends the same thing: a
// content-free wake, carrying no message content or metadata -- exactly
// like freizone-server's existing Web Push path, whose payload is empty
// for the same reason ("the server never sees plaintext", so the
// gateway shouldn't be able to infer anything about it either).
package push

import (
	"context"
	"errors"
)

// ErrTokenInvalid reports that the upstream push service permanently
// rejected the target's token -- the app was uninstalled, its data was
// cleared, or the token belongs to a different sender. It says nothing
// about this particular send attempt (unlike a timeout or a 5xx, which
// are transient): the token itself will never work again, so the caller
// should stop using it rather than retry.
//
// Senders wrap their platform-specific "gone" errors in this so the API
// layer can translate them into one platform-independent signal for
// freizone-server, which then drops the dead registration (see
// handleSendPush).
var ErrTokenInvalid = errors.New("push: token is no longer valid")

// Target identifies where to deliver a wake: which platform, and that
// platform's own addressing token (an FCM registration token, or -- once
// implemented -- an APNs device token).
type Target struct {
	Platform string
	Token    string
}

const (
	PlatformFCM  = "fcm"
	PlatformAPNS = "apns"
)

// Sender delivers a wake to a single Target.
type Sender interface {
	Send(ctx context.Context, target Target) error
}
