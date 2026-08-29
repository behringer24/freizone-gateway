package push

import (
	"context"
	"errors"
	"reflect"
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

// TestFCMSenderKeepsOnlyTokenVariable guards the premise isDeadTokenError
// relies on to treat INVALID_ARGUMENT as a dead token: that Token is the
// only field of the outgoing message that depends on the request. If a
// variable field is ever added here, INVALID_ARGUMENT must come out of
// that classifier -- see its doc comment.
func TestFCMSenderKeepsOnlyTokenVariable(t *testing.T) {
	first := &fakeFCMClient{}
	newFCMSenderWithClient(first).Send(context.Background(), Target{Platform: PlatformFCM, Token: "token-a"})

	second := &fakeFCMClient{}
	newFCMSenderWithClient(second).Send(context.Background(), Target{Platform: PlatformFCM, Token: "token-b"})

	if first.gotMessage == nil || second.gotMessage == nil {
		t.Fatal("fake client was never called")
	}
	if first.gotMessage.Token == second.gotMessage.Token {
		t.Fatal("test is not exercising two different tokens")
	}

	if first.gotMessage.Data["type"] != second.gotMessage.Data["type"] || len(first.gotMessage.Data) != 1 {
		t.Errorf("Data varies with the target or carries extra fields: %v vs %v",
			first.gotMessage.Data, second.gotMessage.Data)
	}
	if !reflect.DeepEqual(first.gotMessage.Android, second.gotMessage.Android) {
		t.Error("Android config varies with the target; INVALID_ARGUMENT is no longer token-specific")
	}
	if first.gotMessage.Notification != nil || first.gotMessage.APNS != nil || first.gotMessage.Webpush != nil {
		t.Error("message carries a field beyond Token/Data/Android; re-check isDeadTokenError")
	}
}

func TestFCMSenderReportsDeadTokenAsErrTokenInvalid(t *testing.T) {
	fake := &fakeFCMClient{err: errors.New("UNREGISTERED")}
	sender := &fcmSender{client: fake, deadToken: func(error) bool { return true }}

	err := sender.Send(context.Background(), Target{Platform: PlatformFCM, Token: "device-token"})
	if !errors.Is(err, ErrTokenInvalid) {
		t.Errorf("Send() error = %v, want it to wrap ErrTokenInvalid", err)
	}
}

func TestFCMSenderKeepsTransientErrorDistinct(t *testing.T) {
	fake := &fakeFCMClient{err: errors.New("UNAVAILABLE")}
	sender := &fcmSender{client: fake, deadToken: func(error) bool { return false }}

	err := sender.Send(context.Background(), Target{Platform: PlatformFCM, Token: "device-token"})
	if err == nil {
		t.Fatal("Send() error = nil, want an error")
	}
	if errors.Is(err, ErrTokenInvalid) {
		t.Error("a transient failure must not be reported as a dead token -- it would drop a live registration")
	}
}

// TestIsDeadTokenErrorIgnoresUnrelatedErrors pins the conservative half of
// the classifier. The positive half cannot be unit-tested from here: the
// SDK carries its error codes on a type in its internal package, so an
// error with a real messagingErrorCode cannot be constructed outside it.
func TestIsDeadTokenErrorIgnoresUnrelatedErrors(t *testing.T) {
	for _, err := range []error{
		errors.New("boom"),
		context.DeadlineExceeded,
		context.Canceled,
		ErrTokenInvalid,
	} {
		if isDeadTokenError(err) {
			t.Errorf("isDeadTokenError(%v) = true, want false -- only real FCM error codes are permanent", err)
		}
	}
}
