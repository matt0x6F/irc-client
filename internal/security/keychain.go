package security

import (
	"fmt"

	"github.com/zalando/go-keyring"
)

const (
	// KeychainService is the service name used for storing passwords in the keychain
	KeychainService = "irc-client"
)

// Keychain provides secure password storage using OS keychain
type Keychain struct{}

// NewKeychain creates a new keychain instance
func NewKeychain() *Keychain {
	return &Keychain{}
}

// StorePassword stores a password for a network in the OS keychain
// user parameter should be the network ID or name
func (k *Keychain) StorePassword(user string, password string) error {
	if password == "" {
		// Empty password, delete instead
		return k.DeletePassword(user)
	}
	err := keyring.Set(KeychainService, user, password)
	if err != nil {
		return fmt.Errorf("failed to store password in keychain: %w", err)
	}
	return nil
}

// GetPassword retrieves a password for a network from the OS keychain
// user parameter should be the network ID or name
func (k *Keychain) GetPassword(user string) (string, error) {
	password, err := keyring.Get(KeychainService, user)
	if err != nil {
		if err == keyring.ErrNotFound {
			return "", nil // Not found is not an error, just return empty
		}
		return "", fmt.Errorf("failed to get password from keychain: %w", err)
	}
	return password, nil
}

// DeletePassword removes a password for a network from the OS keychain
// user parameter should be the network ID or name
func (k *Keychain) DeletePassword(user string) error {
	err := keyring.Delete(KeychainService, user)
	if err != nil {
		if err == keyring.ErrNotFound {
			return nil // Not found is not an error
		}
		return fmt.Errorf("failed to delete password from keychain: %w", err)
	}
	return nil
}

