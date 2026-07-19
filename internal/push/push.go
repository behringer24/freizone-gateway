// Package push abstracts sending a wake notification to a device,
// regardless of platform. Every Sender sends the same thing: a
// content-free wake, carrying no message content or metadata -- exactly
// like freizone-server's existing Web Push path, whose payload is empty
// for the same reason ("the server never sees plaintext", so the
// gateway shouldn't be able to infer anything about it either).
package push

import "context"

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
