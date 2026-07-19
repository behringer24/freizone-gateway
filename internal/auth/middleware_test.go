package auth

import (
	"crypto/ed25519"
	"encoding/base64"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/behringer24/freizone-server/pkg/httpsig"

	"github.com/behringer24/freizone-gateway/internal/revoke"
)

func newTestRevokedList(t *testing.T) *revoke.List {
	t.Helper()
	l, err := revoke.Load(filepath.Join(t.TempDir(), "revoked_keys.txt"))
	if err != nil {
		t.Fatalf("revoke.Load() error = %v", err)
	}
	return l
}

func newCaller(t *testing.T) (pub ed25519.PublicKey, priv ed25519.PrivateKey, keyID string) {
	t.Helper()
	pub, priv, err := ed25519.GenerateKey(nil)
	if err != nil {
		t.Fatalf("GenerateKey() error = %v", err)
	}
	return pub, priv, base64.StdEncoding.EncodeToString(pub)
}

func newSignedRequest(method, path string, body []byte, keyID string, priv ed25519.PrivateKey, ts time.Time, nonce string) *http.Request {
	req := httptest.NewRequest(method, path, strings.NewReader(string(body)))
	sig := httpsig.Sign(method, path, req.URL.RawQuery, body, keyID, ts, nonce, priv)
	req.Header.Set(httpsig.HeaderKeyID, keyID)
	req.Header.Set(httpsig.HeaderTimestamp, httpsig.FormatTimestamp(ts))
	req.Header.Set(httpsig.HeaderNonce, nonce)
	req.Header.Set(httpsig.HeaderSignature, sig)
	return req
}

func TestRequireAcceptsValidRequest(t *testing.T) {
	_, priv, keyID := newCaller(t)
	mw := NewMiddleware(newTestRevokedList(t), nil)

	var gotCaller string
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotCaller, _ = CallerFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	}))

	req := newSignedRequest(http.MethodPost, "/v1/push/send", []byte(`{}`), keyID, priv, time.Now(), "nonce-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200, body = %s", rec.Code, rec.Body.String())
	}
	if gotCaller != keyID {
		t.Errorf("caller = %q, want %q", gotCaller, keyID)
	}
}

func TestRequireRejectsMissingHeaders(t *testing.T) {
	mw := NewMiddleware(newTestRevokedList(t), nil)
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := httptest.NewRequest(http.MethodPost, "/v1/push/send", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireRejectsRevokedKey(t *testing.T) {
	_, priv, keyID := newCaller(t)
	revokedFile := filepath.Join(t.TempDir(), "revoked_keys.txt")
	if err := revoke.Revoke(revokedFile, keyID); err != nil {
		t.Fatalf("revoke.Revoke() error = %v", err)
	}
	revoked, err := revoke.Load(revokedFile)
	if err != nil {
		t.Fatalf("revoke.Load() error = %v", err)
	}

	mw := NewMiddleware(revoked, nil)
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	req := newSignedRequest(http.MethodPost, "/v1/push/send", []byte(`{}`), keyID, priv, time.Now(), "nonce-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireRejectsTamperedBody(t *testing.T) {
	_, priv, keyID := newCaller(t)
	mw := NewMiddleware(newTestRevokedList(t), nil)
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	ts := time.Now()
	nonce := "nonce-1"
	signedBody := []byte(`{"a":1}`)
	sig := httpsig.Sign(http.MethodPost, "/v1/push/send", "", signedBody, keyID, ts, nonce, priv)

	req := httptest.NewRequest(http.MethodPost, "/v1/push/send", strings.NewReader(`{"a":2}`)) // different body than what was signed
	req.Header.Set(httpsig.HeaderKeyID, keyID)
	req.Header.Set(httpsig.HeaderTimestamp, httpsig.FormatTimestamp(ts))
	req.Header.Set(httpsig.HeaderNonce, nonce)
	req.Header.Set(httpsig.HeaderSignature, sig)

	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireRejectsExpiredTimestamp(t *testing.T) {
	_, priv, keyID := newCaller(t)
	mw := NewMiddleware(newTestRevokedList(t), nil)
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	old := time.Now().Add(-10 * time.Minute)
	req := newSignedRequest(http.MethodPost, "/v1/push/send", []byte(`{}`), keyID, priv, old, "nonce-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}

func TestRequireRejectsReplayedNonce(t *testing.T) {
	_, priv, keyID := newCaller(t)
	mw := NewMiddleware(newTestRevokedList(t), nil)
	calls := 0
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.WriteHeader(http.StatusOK)
	}))

	ts := time.Now()
	req1 := newSignedRequest(http.MethodPost, "/v1/push/send", []byte(`{}`), keyID, priv, ts, "same-nonce")
	rec1 := httptest.NewRecorder()
	handler.ServeHTTP(rec1, req1)
	if rec1.Code != http.StatusOK {
		t.Fatalf("first request status = %d, want 200", rec1.Code)
	}

	req2 := newSignedRequest(http.MethodPost, "/v1/push/send", []byte(`{}`), keyID, priv, ts, "same-nonce")
	rec2 := httptest.NewRecorder()
	handler.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusUnauthorized {
		t.Errorf("replayed request status = %d, want 401", rec2.Code)
	}

	if calls != 1 {
		t.Errorf("handler called %d times, want 1", calls)
	}
}

func TestRequireRejectsInvalidKeyID(t *testing.T) {
	_, priv, _ := newCaller(t)
	mw := NewMiddleware(newTestRevokedList(t), nil)
	handler := mw.Require(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler should not be called")
	}))

	// keyID isn't a valid base64-encoded 32-byte Ed25519 public key.
	req := newSignedRequest(http.MethodPost, "/v1/push/send", []byte(`{}`), "not-a-valid-key", priv, time.Now(), "nonce-1")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusUnauthorized {
		t.Errorf("status = %d, want 401", rec.Code)
	}
}
