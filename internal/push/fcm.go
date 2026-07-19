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
	return &fcmSender{client: client}
}

func (s *fcmSender) Send(ctx context.Context, target Target) error {
	_, err := s.client.Send(ctx, &messaging.Message{
		Token: target.Token,
		// "wake" is the only value ever sent -- no message content, no
		// sender/recipient metadata, nothing beyond "go sync", same as
		// the empty payload used on the Web Push path.
		Data:    map[string]string{"type": "wake"},
		Android: &messaging.AndroidConfig{Priority: "high"},
	})
	if err != nil {
		return fmt.Errorf("push: fcm send failed: %w", err)
	}
	return nil
}
