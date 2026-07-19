package auth

import "context"

type contextKey int

const callerContextKey contextKey = iota

// WithCaller returns a context carrying the authenticated caller's
// base64-encoded Ed25519 public key.
func WithCaller(ctx context.Context, callerKey string) context.Context {
	return context.WithValue(ctx, callerContextKey, callerKey)
}

// CallerFromContext retrieves the caller key injected by Middleware.Require.
// ok is false if the request was never authenticated.
func CallerFromContext(ctx context.Context) (string, bool) {
	key, ok := ctx.Value(callerContextKey).(string)
	return key, ok
}
