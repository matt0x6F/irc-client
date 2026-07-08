package security

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

// testKey is a fixed 32-byte AES-256 key for deterministic tests.
var testKey = bytes.Repeat([]byte{0x42}, 32)

// FileBackend must satisfy SecretBackend: round-trip a value, report "" (no
// error) for an absent key, and treat deleting a missing key as a no-op.
func TestFileBackendImplementsSecretBackend(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")
	var _ SecretBackend = NewFileBackendWithKey(path, testKey)

	f := NewFileBackendWithKey(path, testKey)
	if err := f.Set("k1", "v1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, err := f.Get("k1"); err != nil || got != "v1" {
		t.Fatalf("Get(k1) = %q, %v; want %q, nil", got, err, "v1")
	}
	if got, err := f.Get("missing"); err != nil || got != "" {
		t.Fatalf("Get(missing) = %q, %v; want \"\", nil", got, err)
	}
	if err := f.Delete("k1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := f.Get("k1"); got != "" {
		t.Fatalf("Get after delete = %q; want \"\"", got)
	}
	if err := f.Delete("missing"); err != nil {
		t.Fatalf("Delete(missing) should be a no-op, got %v", err)
	}
}

// The core regression guard for the ad-hoc-signing bug: a secret written by one
// backend instance MUST be readable by a completely fresh instance pointed at
// the same file with the same key. This is precisely the guarantee the macOS
// keychain fails after an app update (new cdhash -> new designated requirement
// -> read denied). The file store depends only on the file and key, never on
// the process/binary identity, so it survives updates.
func TestFileBackendSurvivesFreshInstance(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")

	writer := NewFileBackendWithKey(path, testKey)
	if err := writer.Set("network-1-sasl_password", "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	// A brand-new instance (simulating the updated binary) with the same key.
	reader := NewFileBackendWithKey(path, testKey)
	if got, err := reader.Get("network-1-sasl_password"); err != nil || got != "hunter2" {
		t.Fatalf("fresh instance Get = %q, %v; want %q, nil", got, err, "hunter2")
	}
}

// Multiple keys coexist and updates replace in place.
func TestFileBackendMultipleKeysAndUpdate(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")
	f := NewFileBackendWithKey(path, testKey)

	if err := f.Set("a", "1"); err != nil {
		t.Fatalf("Set a: %v", err)
	}
	if err := f.Set("b", "2"); err != nil {
		t.Fatalf("Set b: %v", err)
	}
	if err := f.Set("a", "updated"); err != nil {
		t.Fatalf("update a: %v", err)
	}

	f2 := NewFileBackendWithKey(path, testKey)
	if got, _ := f2.Get("a"); got != "updated" {
		t.Fatalf("Get(a) = %q; want %q", got, "updated")
	}
	if got, _ := f2.Get("b"); got != "2" {
		t.Fatalf("Get(b) = %q; want %q", got, "2")
	}
}

// Secrets must be encrypted at rest: the plaintext value must not appear in the
// on-disk file.
func TestFileBackendEncryptsAtRest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")
	f := NewFileBackendWithKey(path, testKey)
	if err := f.Set("network-1-sasl_password", "supersecret"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if bytes.Contains(raw, []byte("supersecret")) {
		t.Fatal("on-disk file contains the plaintext secret; not encrypted")
	}
	if info, err := os.Stat(path); err == nil {
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("file perms = %o; want 600", perm)
		}
	}
}

// A wrong key must fail to decrypt rather than silently returning garbage or
// the wrong plaintext.
func TestFileBackendWrongKeyFailsToDecrypt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "credentials.enc")
	if err := NewFileBackendWithKey(path, testKey).Set("k", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	wrongKey := bytes.Repeat([]byte{0x99}, 32)
	if _, err := NewFileBackendWithKey(path, wrongKey).Get("k"); err == nil {
		t.Fatal("Get with wrong key should error, got nil")
	}
}

// The production constructor derives its key from a persisted master key, so a
// secret written by one instance must be readable by a freshly constructed
// instance over the same dataDir. If the derived key weren't stable, every
// launch (or update) would fail to decrypt — reintroducing the very bug this
// replaces.
func TestNewFileBackendKeyStableAcrossInstances(t *testing.T) {
	dir := t.TempDir()

	b1, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("NewFileBackend #1: %v", err)
	}
	if err := b1.Set("network-1-sasl_password", "hunter2"); err != nil {
		t.Fatalf("Set: %v", err)
	}

	b2, err := NewFileBackend(dir)
	if err != nil {
		t.Fatalf("NewFileBackend #2: %v", err)
	}
	if got, err := b2.Get("network-1-sasl_password"); err != nil || got != "hunter2" {
		t.Fatalf("fresh instance Get = %q, %v; want %q, nil", got, err, "hunter2")
	}
}

// The persisted master key file must be 0600 so other users can't read it.
func TestNewFileBackendMasterKeyPerms(t *testing.T) {
	dir := t.TempDir()
	if _, err := NewFileBackend(dir); err != nil {
		t.Fatalf("NewFileBackend: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, "credentials.key"))
	if err != nil {
		t.Fatalf("master key file not created: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("master key perms = %o; want 600", perm)
	}
}

// NewSecretBackend is the production factory; the returned value must satisfy
// SecretBackend and round-trip.
func TestNewSecretBackendRoundTrips(t *testing.T) {
	dir := t.TempDir()
	b, err := NewSecretBackend(dir)
	if err != nil {
		t.Fatalf("NewSecretBackend: %v", err)
	}
	var _ SecretBackend = b
	if err := b.Set("k", "v"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, _ := b.Get("k"); got != "v" {
		t.Fatalf("Get = %q; want v", got)
	}
}
