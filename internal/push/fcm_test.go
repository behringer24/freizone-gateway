package push

import (
	"context"
	"errors"
	"testing"

	"firebase.google.com/go/v4/messaging"
)

type fakeFCMClient struct {
	gotMessage *messaging.Message
	err        error
}

func (f *fakeFCMClient) Send(ctx context.Context, message *messaging.Message) (string, error) {
	f.gotMessage = message
	if f.err != nil {
		return "", f.err
	}
	return "message-id", nil
}

func TestFCMSenderSendsContentFreeWake(t *testing.T) {
	fake := &fakeFCMClient{}
	sender := newFCMSenderWithClient(fake)

	if err := sender.Send(context.Background(), Target{Platform: PlatformFCM, Token: "device-token"}); err != nil {
		t.Fatalf("Send() error = %v", err)
	}

	if fake.gotMessage == nil {
		t.Fatal("fake client was never called")
	}
	if fake.gotMessage.Token != "device-token" {
		t.Errorf("Token = %q, want %q", fake.gotMessage.Token, "device-token")
	}
	if fake.gotMessage.Notification != nil {
		t.Error("Notification should be nil -- this must be a silent, content-free wake")
	}
	if len(fake.gotMessage.Data) == 0 {
		t.Error("Data should carry at least a minimal wake marker so FCM doesn't reject an empty message")
	}
}

func TestFCMSenderWrapsUpstreamError(t *testing.T) {
	fake := &fakeFCMClient{err: errors.New("boom")}
	sender := newFCMSenderWithClient(fake)

	err := sender.Send(context.Background(), Target{Platform: PlatformFCM, Token: "device-token"})
	if err == nil {
		t.Fatal("Send() error = nil, want an error")
	}
}
