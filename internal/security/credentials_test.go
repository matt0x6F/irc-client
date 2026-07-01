package security

import (
	"errors"
	"testing"
)

// fakeBackend is an in-memory SecretBackend with an optional forced Set error
// to exercise the keychain-unavailable fallback path.
type fakeBackend struct {
	m      map[string]string
	setErr error
}

func newFakeBackend() *fakeBackend { return &fakeBackend{m: map[string]string{}} }

func (f *fakeBackend) Set(key, value string) error {
	if f.setErr != nil {
		return f.setErr
	}
	f.m[key] = value
	return nil
}
func (f *fakeBackend) Get(key string) (string, error) { return f.m[key], nil }
func (f *fakeBackend) Delete(key string) error        { delete(f.m, key); return nil }

func TestCredentialStoreStoreAndResolve(t *testing.T) {
	cs := NewCredentialStore(newFakeBackend())

	used, err := cs.Store(7, FieldPassword, "hunter2")
	if err != nil || !used {
		t.Fatalf("Store: used=%v err=%v", used, err)
	}
	// Keychain value wins over any column value.
	if got := cs.Resolve(7, FieldPassword, "STALE-COLUMN"); got != "hunter2" {
		t.Errorf("Resolve = %q, want %q", got, "hunter2")
	}
	// Scoped per network: another id resolves to its column fallback.
	if got := cs.Resolve(8, FieldPassword, "other"); got != "other" {
		t.Errorf("Resolve(other net) = %q, want %q", got, "other")
	}
}

func TestCredentialStoreResolveFallsBackToColumn(t *testing.T) {
	cs := NewCredentialStore(newFakeBackend())
	if got := cs.Resolve(1, FieldSASLPassword, "plaintext-legacy"); got != "plaintext-legacy" {
		t.Errorf("Resolve = %q, want column fallback", got)
	}
}

func TestCredentialStoreStoreEmptyDeletes(t *testing.T) {
	b := newFakeBackend()
	cs := NewCredentialStore(b)
	if _, err := cs.Store(3, FieldPassword, "secret"); err != nil {
		t.Fatal(err)
	}
	if _, err := cs.Store(3, FieldPassword, ""); err != nil {
		t.Fatal(err)
	}
	if _, ok := b.m[credKey(3, FieldPassword)]; ok {
		t.Errorf("empty Store should delete the keychain entry")
	}
}

func TestCredentialStoreStoreBackendErrorFallsBack(t *testing.T) {
	b := newFakeBackend()
	b.setErr = errors.New("keyring unavailable")
	cs := NewCredentialStore(b)

	used, err := cs.Store(5, FieldPassword, "secret")
	if used {
		t.Errorf("used=true, want false when backend fails")
	}
	if err == nil {
		t.Errorf("expected error surfaced from backend failure")
	}
}

func TestCredentialStoreMigrate(t *testing.T) {
	b := newFakeBackend()
	cs := NewCredentialStore(b)

	moved, err := cs.Migrate(9, FieldSASLExternalCert, "legacy-cert")
	if err != nil || !moved {
		t.Fatalf("Migrate: moved=%v err=%v", moved, err)
	}
	// After migration the secret is in the keychain; a blanked column still resolves.
	if got := cs.Resolve(9, FieldSASLExternalCert, ""); got != "legacy-cert" {
		t.Errorf("post-migrate Resolve = %q, want %q", got, "legacy-cert")
	}
	// Nothing to migrate when the column is empty.
	moved, err = cs.Migrate(9, FieldPassword, "")
	if moved || err != nil {
		t.Errorf("empty column: moved=%v err=%v, want false/nil", moved, err)
	}
}
