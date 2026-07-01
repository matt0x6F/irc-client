package security

import "fmt"

// Secret field identifiers. These form part of the per-network keychain key, so
// they must stay stable once released.
const (
	FieldPassword         = "password"
	FieldSASLPassword     = "sasl_password"
	FieldSASLExternalCert = "sasl_external_cert"
)

// SecretBackend is the minimal storage surface CredentialStore needs. Get
// returns "" (no error) when the key is absent; Delete of a missing key is a
// no-op. *Keychain satisfies this over the OS keychain.
type SecretBackend interface {
	Set(key, value string) error
	Get(key string) (string, error)
	Delete(key string) error
}

// CredentialStore stores per-network IRC secrets in a SecretBackend (the OS
// keychain in production). It keeps secrets out of the SQLite database, and
// helps migrate legacy plaintext rows into the keychain lazily.
type CredentialStore struct {
	backend SecretBackend
}

// NewCredentialStore wraps a SecretBackend.
func NewCredentialStore(backend SecretBackend) *CredentialStore {
	return &CredentialStore{backend: backend}
}

// credKey namespaces a secret by network id and field.
func credKey(networkID int64, field string) string {
	return fmt.Sprintf("network-%d-%s", networkID, field)
}

// Store persists a secret for (networkID, field). An empty value deletes the
// entry. usedKeychain reports whether the backend accepted the write; on a
// backend failure it is false and err is set, so the caller can fall back to
// plaintext column storage.
func (cs *CredentialStore) Store(networkID int64, field, value string) (usedKeychain bool, err error) {
	if cs == nil {
		return false, nil // no keychain configured: caller falls back to the column
	}
	key := credKey(networkID, field)
	if value == "" {
		if err := cs.backend.Delete(key); err != nil {
			return false, err
		}
		return true, nil
	}
	if err := cs.backend.Set(key, value); err != nil {
		return false, err
	}
	return true, nil
}

// Resolve returns the effective secret for (networkID, field): the keychain
// value if present, otherwise the provided columnValue (a legacy plaintext row
// not yet migrated). A backend error degrades to the column value.
func (cs *CredentialStore) Resolve(networkID int64, field, columnValue string) string {
	if cs == nil {
		return columnValue
	}
	if v, err := cs.backend.Get(credKey(networkID, field)); err == nil && v != "" {
		return v
	}
	return columnValue
}

// Migrate relocates a legacy plaintext columnValue into the keychain. It returns
// moved=true when the secret now lives in the keychain (so the caller should
// blank the SQLite column). An empty columnValue is a no-op. If the secret is
// already in the keychain, it reports moved=true without rewriting.
func (cs *CredentialStore) Migrate(networkID int64, field, columnValue string) (moved bool, err error) {
	if cs == nil || columnValue == "" {
		return false, nil
	}
	if existing, gerr := cs.backend.Get(credKey(networkID, field)); gerr == nil && existing != "" {
		return true, nil
	}
	used, err := cs.Store(networkID, field, columnValue)
	if err != nil || !used {
		return false, err
	}
	return true, nil
}

// Delete removes all secrets for a network (used when the network is deleted).
func (cs *CredentialStore) Delete(networkID int64) error {
	if cs == nil {
		return nil
	}
	for _, field := range []string{FieldPassword, FieldSASLPassword, FieldSASLExternalCert} {
		if err := cs.backend.Delete(credKey(networkID, field)); err != nil {
			return err
		}
	}
	return nil
}
