// Package revoke implements the gateway's caller revocation list: any
// freizone-server can call POST /v1/push/send without prior registration
// (see internal/auth), signing with its own self-generated Ed25519
// identity. If a specific caller turns out to be abusive, the gateway
// operator revokes that exact public key -- this package is where that
// list lives.
//
// Deliberately a flat file, not a database: the gateway otherwise holds
// no durable state at all (see internal/auth's in-memory nonce cache),
// and a revocation list an operator can inspect/edit directly (one
// base64 key per line) is easier to reason about than a schema for what
// is, in practice, a handful of entries.
package revoke

import (
	"bufio"
	"fmt"
	"os"
	"strings"
	"sync"
)

// List is a reloadable set of revoked base64-encoded Ed25519 public
// keys. Safe for concurrent use.
type List struct {
	path string

	mu      sync.RWMutex
	revoked map[string]struct{}
}

// Load reads path (if it exists -- a missing file just means "nothing
// revoked yet") into a new List.
func Load(path string) (*List, error) {
	l := &List{path: path, revoked: map[string]struct{}{}}
	if err := l.Reload(); err != nil {
		return nil, err
	}
	return l, nil
}

// Reload re-reads the revocation file from disk, replacing the current
// in-memory set. Call this periodically (see cmd/gateway) so an operator
// revoking a key takes effect without a restart.
func (l *List) Reload() error {
	f, err := os.Open(l.path)
	if os.IsNotExist(err) {
		l.mu.Lock()
		l.revoked = map[string]struct{}{}
		l.mu.Unlock()
		return nil
	}
	if err != nil {
		return fmt.Errorf("revoke: opening %s: %w", l.path, err)
	}
	defer f.Close()

	fresh := map[string]struct{}{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fresh[line] = struct{}{}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("revoke: reading %s: %w", l.path, err)
	}

	l.mu.Lock()
	l.revoked = fresh
	l.mu.Unlock()
	return nil
}

// IsRevoked reports whether the given base64-encoded public key is on
// the revocation list.
func (l *List) IsRevoked(base64Key string) bool {
	l.mu.RLock()
	defer l.mu.RUnlock()
	_, ok := l.revoked[base64Key]
	return ok
}

// Revoke appends base64Key to the revocation file, if it isn't already
// present, and reloads the in-memory set. Used by the gateway binary's
// -revoke-key flag.
func Revoke(path, base64Key string) error {
	l, err := Load(path)
	if err != nil {
		return err
	}
	if l.IsRevoked(base64Key) {
		return nil
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return fmt.Errorf("revoke: opening %s for append: %w", path, err)
	}
	defer f.Close()

	if _, err := fmt.Fprintln(f, base64Key); err != nil {
		return fmt.Errorf("revoke: appending to %s: %w", path, err)
	}
	return nil
}
