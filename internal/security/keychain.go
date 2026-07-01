package security

import (
	"errors"
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	// KeychainService is the service name used for storing secrets in the OS keychain.
	KeychainService = "cascade-chat"
)

// Keychain provides secure secret storage using the OS keychain. It implements
// SecretBackend.
type Keychain struct{}

// NewKeychain creates a new keychain instance.
func NewKeychain() *Keychain {
	return &Keychain{}
}

// Set stores value under key in the OS keychain.
func (k *Keychain) Set(key, value string) error {
	if err := keyring.Set(KeychainService, key, value); err != nil {
		return fmt.Errorf("failed to store secret in keychain: %w", err)
	}
	return nil
}

// Get retrieves the secret stored under key. A missing key returns "" with no
// error.
func (k *Keychain) Get(key string) (string, error) {
	value, err := keyring.Get(KeychainService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("failed to get secret from keychain: %w", err)
	}
	return value, nil
}

// Delete removes the secret stored under key. A missing key is a no-op.
func (k *Keychain) Delete(key string) error {
	err := keyring.Delete(KeychainService, key)
	if err != nil {
		if errors.Is(err, keyring.ErrNotFound) {
			return nil
		}
		return fmt.Errorf("failed to delete secret from keychain: %w", err)
	}
	return nil
}
