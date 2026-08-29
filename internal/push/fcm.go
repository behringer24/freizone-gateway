package push

import (
	"context"
	"fmt"

	firebase "firebase.google.com/go/v4"
	"firebase.google.com/go/v4/messaging"
	"google.golang.org/api/option"
)

// fcmClient is the subset of *messaging.Client this package calls,
// so tests can substitute a fake instead of talking to real FCM.
type fcmClient interface {
	Send(ctx context.Context, message *messaging.Message) (string, error)
}

type fcmSender struct {
	client fcmClient
	// deadToken classifies an upstream error as permanently fatal for
	// this token. It is a field rather than a direct call because the
	// SDK carries its error codes on a type in its own internal package,
	// which cannot be constructed from outside it -- so this is the only
	// seam a test can reach the mapping through.
	deadToken func(error) bool
}

// NewFCMSender builds a Sender backed by the Firebase Admin Go SDK,
// authenticated with the service-account key at credentialsFile.
func NewFCMSender(ctx context.Context, credentialsFile string) (Sender, error) {
	app, err := firebase.NewApp(ctx, nil, option.WithCredentialsFile(credentialsFile))
	if err != nil {
		return nil, fmt.Errorf("push: initializing firebase app: %w", err)
	}
	client, err := app.Messaging(ctx)
	if err != nil {
		return nil, fmt.Errorf("push: initializing fcm client: %w", err)
	}
	return newFCMSenderWithClient(client), nil
}

func newFCMSenderWithClient(client fcmClient) Sender {
	return &fcmSender{client: client, deadToken: isDeadTokenError}
}

// isDeadTokenError reports whether err means this token will never work
// again, as opposed to this attempt having failed.
//
// UNREGISTERED means the app was uninstalled or its data cleared.
// SENDER_ID_MISMATCH means the token belongs to a different Firebase
// project. Both are unambiguously permanent for the token alone.
//
// INVALID_ARGUMENT is different in kind: it is FCM's generic 400 for the
// request as a whole, and would also cover a malformed payload. Counting
// it here is sound only because the message built in Send has exactly one
// variable field -- Token. Data and Android are compile-time constants,
// and nothing else is derived from the caller's request. A broken
// constant would therefore fail every send rather than one, which is a
// deployment-wide outage to notice, not a token to discard.
//
// That premise is load-bearing: if a variable field is ever added to the
// message, INVALID_ARGUMENT stops being token-specific and must be taken
// out of this list. Leaving it in would make one malformed payload cause
// every server pointed at this gateway to drop every registration it has.
//
// Everything else (quota, unavailable, internal, third-party auth) is
// transient or on our side, and leaves the token alone.
func isDeadTokenError(err error) bool {
	return messaging.IsUnregistered(err) ||
		messaging.IsSenderIDMismatch(err) ||
		messaging.IsInvalidArgument(err)
}

func (s *fcmSender) Send(ctx context.Context, target Target) error {
	_, err := s.client.Send(ctx, &messaging.Message{
		Token: target.Token,
		// "wake" is the only value ever sent -- no message content, no
		// sender/recipient metadata, nothing beyond "go sync", same as
		// the empty payload used on the Web Push path.
		//
		// Keep these two constant: isDeadTokenError's treatment of
		// INVALID_ARGUMENT depends on Token being the only field that
		// can vary.
		Data:    map[string]string{"type": "wake"},
		Android: &messaging.AndroidConfig{Priority: "high"},
	})
	if err != nil {
		// A permanently dead token is reported as ErrTokenInvalid so the
		// calling server drops the registration instead of waking a
		// device that no longer exists on every message.
		if s.deadToken(err) {
			return fmt.Errorf("%w: %v", ErrTokenInvalid, err)
		}
		return fmt.Errorf("push: fcm send failed: %w", err)
	}
	return nil
}
