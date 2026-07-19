// Package auth authenticates gateway requests using the same per-request
// Ed25519 signature scheme freizone-server uses for device auth
// (pkg/httpsig), with one deliberate difference: there is no caller
// registry. Signature-Key-Id is the caller's own base64-encoded public
// key -- self-describing, so any freizone-server can call in the moment
// it mints its own identity, with no prior handshake. The only local
// state is a revocation list (internal/revoke) an operator can add a key
// to if a caller turns out to be abusive.
package auth

import (
	"bytes"
	"crypto/ed25519"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"

	"github.com/behringer24/freizone-server/pkg/httpsig"

	"github.com/behringer24/freizone-gateway/internal/revoke"
)

// MaxClockSkew mirrors freizone-server's internal/auth constant of the
// same name -- both sides of the wire contract must agree on it.
const MaxClockSkew = 5 * time.Minute

// Middleware authenticates incoming requests against the signing
// caller's own embedded public key.
type Middleware struct {
	Revoked *revoke.List
	Logger  *slog.Logger
	// Now returns the current time; overridable in tests.
	Now func() time.Time

	nonces *nonceCache
}

// NewMiddleware builds a Middleware backed by revoked, logging
// authentication failures (at Warn level, with detail) to logger.
func NewMiddleware(revoked *revoke.List, logger *slog.Logger) *Middleware {
	return &Middleware{Revoked: revoked, Logger: logger, Now: time.Now, nonces: newNonceCache()}
}

// SweepNonces prunes expired replay-guard entries. Call periodically
// (see cmd/gateway) -- the cache never persists across restarts, so
// there's no requirement it be called before shutdown.
func (m *Middleware) SweepNonces() {
	m.nonces.sweep(m.Now())
}

// Require wraps next so it only runs for requests with a valid, non-
// revoked signature, injecting the caller's key into the request
// context. Every failure mode (bad key, bad signature, expired
// timestamp, replayed nonce, revoked key) produces the same generic 401
// response, so as not to give an attacker an oracle; specifics go only
// to the log.
func (m *Middleware) Require(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callerKey, err := m.authenticate(r)
		if err != nil {
			if m.Logger != nil {
				m.Logger.Warn("request authentication failed", "error", err, "path", r.URL.Path)
			}
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":{"code":"unauthorized","message":"authentication failed"}}`))
			return
		}
		next.ServeHTTP(w, r.WithContext(WithCaller(r.Context(), callerKey)))
	})
}

func (m *Middleware) authenticate(r *http.Request) (string, error) {
	headers, err := httpsig.ParseRequestHeaders(r)
	if err != nil {
		return "", err
	}

	ts, err := httpsig.ParseTimestamp(headers.Timestamp)
	if err != nil {
		return "", err
	}
	now := m.Now()
	if !httpsig.WithinSkew(ts, now, MaxClockSkew) {
		return "", errors.New("auth: timestamp outside allowed skew")
	}

	pubKeyBytes, err := base64.StdEncoding.DecodeString(headers.KeyID)
	if err != nil || len(pubKeyBytes) != ed25519.PublicKeySize {
		return "", fmt.Errorf("auth: invalid caller key: %w", err)
	}

	if m.Revoked != nil && m.Revoked.IsRevoked(headers.KeyID) {
		return "", errors.New("auth: caller key is revoked")
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return "", fmt.Errorf("auth: reading body: %w", err)
	}
	r.Body = io.NopCloser(bytes.NewReader(body))

	canonical := httpsig.CanonicalStringFromRequest(r, headers, body)
	if err := httpsig.Verify(canonical, headers.Signature, ed25519.PublicKey(pubKeyBytes)); err != nil {
		return "", err
	}

	// expiresAt mirrors freizone-server's own nonce bookkeeping: once
	// real time has moved this far past ts, a replay of this exact
	// timestamp would already be rejected by the skew check above,
	// making the cache entry safe to prune.
	if !m.nonces.recordIfNew(headers.KeyID, headers.Nonce, ts.Add(MaxClockSkew)) {
		return "", errors.New("auth: replayed nonce")
	}

	return headers.KeyID, nil
}
