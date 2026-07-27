package api

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/behringer24/freizone-server/pkg/httpsig"

	gwauth "github.com/behringer24/freizone-gateway/internal/auth"
	"github.com/behringer24/freizone-gateway/internal/push"
	"github.com/behringer24/freizone-gateway/internal/revoke"
)

type fakeSender struct {
	err error
	got push.Target
}

func (f *fakeSender) Send(ctx context.Context, target push.Target) error {
	f.got = target
	return f.err
}

func newTestAPI(t *testing.T, senders map[string]push.Sender, capabilities map[string]bool) *API {
	t.Helper()
	revoked, err := revoke.Load(filepath.Join(t.TempDir(), "revoked_keys.txt"))
	if err != nil {
		t.Fatalf("revoke.Load() error = %v", err)
	}
	return New(gwauth.NewMiddleware(revoked, nil), senders, capabilities, nil)
}

func newSignedPushRequest(t *testing.T, body string) *http.Request {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	keyID := base64.StdEncoding.EncodeToString(pub)
	ts := time.Now()
	nonce := "nonce-1"

	req := httptest.NewRequest(http.MethodPost, "/v1/push/send", strings.NewReader(body))
	sig := httpsig.Sign(http.MethodPost, "/v1/push/send", "", []byte(body), keyID, ts, nonce, priv)
	req.Header.Set(httpsig.HeaderKeyID, keyID)
	req.Header.Set(httpsig.HeaderTimestamp, httpsig.FormatTimestamp(ts))
	req.Header.Set(httpsig.HeaderNonce, nonce)
	req.Header.Set(httpsig.HeaderSignature, sig)
	return req
}

func TestHandleHealth(t *testing.T) {
	a := newTestAPI(t, nil, nil)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/healthz", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
}

func TestHandleGetCapabilities(t *testing.T) {
	a := newTestAPI(t, nil, map[string]bool{push.PlatformFCM: true, push.PlatformAPNS: false})
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/v1/capabilities", nil))

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, `"fcm":true`) || !strings.Contains(body, `"apns":false`) {
		t.Errorf("body = %s, want fcm:true and apns:false", body)
	}
}

func TestHandleSendPushRequiresAuth(t *testing.T) {
	sender := &fakeSender{}
	a := newTestAPI(t, map[string]push.Sender{push.PlatformFCM: sender}, map[string]bool{push.PlatformFCM: true})

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/push/send", strings.NewReader(`{"platform":"fcm","token":"tok"}`))
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestHandleSendPushDispatchesToConfiguredSender(t *testing.T) {
	sender := &fakeSender{}
	a := newTestAPI(t, map[string]push.Sender{push.PlatformFCM: sender}, map[string]bool{push.PlatformFCM: true})

	req := newSignedPushRequest(t, `{"platform":"fcm","token":"device-token"}`)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if sender.got.Token != "device-token" {
		t.Errorf("sender got token = %q, want %q", sender.got.Token, "device-token")
	}
}

func TestHandleSendPushRejectsUnknownPlatform(t *testing.T) {
	a := newTestAPI(t, map[string]push.Sender{}, map[string]bool{})

	req := newSignedPushRequest(t, `{"platform":"carrier-pigeon","token":"device-token"}`)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}

func TestHandleSendPushRejectsUnconfiguredPlatform(t *testing.T) {
	a := newTestAPI(t, map[string]push.Sender{push.PlatformAPNS: push.NewAPNSSender()}, map[string]bool{push.PlatformAPNS: false})

	req := newSignedPushRequest(t, `{"platform":"apns","token":"device-token"}`)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501", rec.Code)
	}
}

// A known platform with no Senders entry at all (e.g. fcm with no
// GATEWAY_FCM_CREDENTIALS_FILE, matching cmd/gateway's real wiring) must
// still be reported as "not configured" (501), not "unknown" (400) --
// regression coverage for a bug caught during manual end-to-end testing.
func TestHandleSendPushRejectsUnconfiguredPlatformNotInSendersMap(t *testing.T) {
	a := newTestAPI(t, map[string]push.Sender{}, map[string]bool{push.PlatformFCM: false})

	req := newSignedPushRequest(t, `{"platform":"fcm","token":"device-token"}`)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusNotImplemented {
		t.Errorf("status = %d, want 501, body = %s", rec.Code, rec.Body.String())
	}
}

func TestHandleSendPushReturnsBadGatewayOnSendFailure(t *testing.T) {
	sender := &fakeSender{err: errors.New("upstream boom")}
	a := newTestAPI(t, map[string]push.Sender{push.PlatformFCM: sender}, map[string]bool{push.PlatformFCM: true})

	req := newSignedPushRequest(t, `{"platform":"fcm","token":"device-token"}`)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", rec.Code)
	}
}

// A permanently dead token must be reported as 410 Gone with a distinct
// code, not folded into the generic 502 -- that difference is what lets
// freizone-server tell "retry later" apart from "drop this registration".
func TestHandleSendPushReturnsGoneOnInvalidToken(t *testing.T) {
	sender := &fakeSender{err: fmt.Errorf("%w: registration-token-not-registered", push.ErrTokenInvalid)}
	a := newTestAPI(t, map[string]push.Sender{push.PlatformFCM: sender}, map[string]bool{push.PlatformFCM: true})

	req := newSignedPushRequest(t, `{"platform":"fcm","token":"dead-token"}`)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusGone {
		t.Fatalf("status = %d, want 410, body = %s", rec.Code, rec.Body.String())
	}
	if body := rec.Body.String(); !strings.Contains(body, "token_invalid") {
		t.Errorf("body = %s, want it to carry the token_invalid code", body)
	}
}

func TestHandleSendPushRejectsMissingToken(t *testing.T) {
	a := newTestAPI(t, map[string]push.Sender{push.PlatformFCM: &fakeSender{}}, map[string]bool{push.PlatformFCM: true})

	req := newSignedPushRequest(t, `{"platform":"fcm"}`)
	rec := httptest.NewRecorder()
	a.Router().ServeHTTP(rec, req)

	if rec.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", rec.Code)
	}
}
