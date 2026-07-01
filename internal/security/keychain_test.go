package security

import (
	"testing"

	"github.com/zalando/go-keyring"
)

// Keychain must satisfy SecretBackend over the OS keychain: round-trip a value,
// report "" (no error) for an absent key, and treat deleting a missing key as a
// no-op.
func TestKeychainImplementsSecretBackend(t *testing.T) {
	keyring.MockInit()
	var _ SecretBackend = NewKeychain()

	k := NewKeychain()
	if err := k.Set("k1", "v1"); err != nil {
		t.Fatalf("Set: %v", err)
	}
	if got, err := k.Get("k1"); err != nil || got != "v1" {
		t.Fatalf("Get(k1) = %q, %v; want %q, nil", got, err, "v1")
	}
	if got, err := k.Get("missing"); err != nil || got != "" {
		t.Fatalf("Get(missing) = %q, %v; want \"\", nil", got, err)
	}
	if err := k.Delete("k1"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if got, _ := k.Get("k1"); got != "" {
		t.Fatalf("Get after delete = %q; want \"\"", got)
	}
	if err := k.Delete("missing"); err != nil {
		t.Fatalf("Delete(missing) should be a no-op, got %v", err)
	}
}
