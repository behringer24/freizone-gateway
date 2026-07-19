package revoke

import (
	"path/filepath"
	"testing"
)

func TestLoadMissingFileIsEmpty(t *testing.T) {
	l, err := Load(filepath.Join(t.TempDir(), "revoked_keys.txt"))
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if l.IsRevoked("anything") {
		t.Error("IsRevoked() = true on an empty list")
	}
}

func TestRevokeThenIsRevoked(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revoked_keys.txt")
	if err := Revoke(path, "abc123"); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}

	l, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !l.IsRevoked("abc123") {
		t.Error("IsRevoked() = false, want true after Revoke()")
	}
	if l.IsRevoked("someone-else") {
		t.Error("IsRevoked() = true for a key that was never revoked")
	}
}

func TestRevokeIsIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revoked_keys.txt")
	if err := Revoke(path, "abc123"); err != nil {
		t.Fatalf("first Revoke() error = %v", err)
	}
	if err := Revoke(path, "abc123"); err != nil {
		t.Fatalf("second Revoke() error = %v", err)
	}

	l, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if !l.IsRevoked("abc123") {
		t.Error("IsRevoked() = false after duplicate Revoke() calls")
	}
}

func TestReloadPicksUpNewRevocations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "revoked_keys.txt")
	l, err := Load(path)
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if l.IsRevoked("abc123") {
		t.Fatal("IsRevoked() = true before Revoke()")
	}

	if err := Revoke(path, "abc123"); err != nil {
		t.Fatalf("Revoke() error = %v", err)
	}
	if l.IsRevoked("abc123") {
		t.Error("IsRevoked() = true before Reload()")
	}

	if err := l.Reload(); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if !l.IsRevoked("abc123") {
		t.Error("IsRevoked() = false after Reload(), want true")
	}
}
