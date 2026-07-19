package push

import (
	"context"
	"errors"
	"testing"
)

func TestAPNSSenderReturnsNotImplemented(t *testing.T) {
	sender := NewAPNSSender()
	err := sender.Send(context.Background(), Target{Platform: PlatformAPNS, Token: "device-token"})
	if !errors.Is(err, ErrAPNSNotImplemented) {
		t.Errorf("Send() error = %v, want ErrAPNSNotImplemented", err)
	}
}
