package security

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"golang.org/x/crypto/hkdf"
)

const (
	credentialsFileName = "credentials.enc"
	masterKeyFileName   = "credentials.key"
	// hkdfInfo domain-separates this key derivation; bump the suffix only if the
	// derivation scheme changes in an incompatible way.
	hkdfInfo = "cascade-chat-credentials-v1"
)

// NewSecretBackend builds the production SecretBackend rooted at dataDir. It
// uses the encrypted-file store on every platform: unlike the OS keychain, its
// access depends only on files under dataDir, so secrets survive app updates on
// ad-hoc-signed builds (whose per-binary designated requirement changes every
// release and would otherwise lock the keychain out).
func NewSecretBackend(dataDir string) (*FileBackend, error) {
	return NewFileBackend(dataDir)
}

// NewFileBackend builds a FileBackend rooted at dataDir, deriving a stable
// encryption key from a persisted per-install master key.
func NewFileBackend(dataDir string) (*FileBackend, error) {
	key, err := deriveFileKey(dataDir)
	if err != nil {
		return nil, err
	}
	return NewFileBackendWithKey(filepath.Join(dataDir, credentialsFileName), key), nil
}

// deriveFileKey produces the 32-byte AES key from the persisted master key,
// mixing in a best-effort machine identifier as the HKDF salt. The machine
// binding means a stolen credentials.key + credentials.enc pair can't be
// decrypted on a different machine; an empty id (unsupported OS) simply omits
// that extra binding without weakening the at-rest encryption.
func deriveFileKey(dataDir string) ([]byte, error) {
	master, err := loadOrCreateMasterKey(filepath.Join(dataDir, masterKeyFileName))
	if err != nil {
		return nil, err
	}
	r := hkdf.New(sha256.New, master, []byte(machineID()), []byte(hkdfInfo))
	key := make([]byte, 32)
	if _, err := io.ReadFull(r, key); err != nil {
		return nil, fmt.Errorf("derive credentials key: %w", err)
	}
	return key, nil
}

// loadOrCreateMasterKey returns the 32-byte master key at path, creating it with
// cryptographically random bytes (0600) on first use. It uses O_EXCL so two
// instances racing at startup can't clobber each other's key.
func loadOrCreateMasterKey(path string) ([]byte, error) {
	if b, err := os.ReadFile(path); err == nil {
		if len(b) != 32 {
			return nil, fmt.Errorf("master key file %s is corrupt (%d bytes, want 32)", path, len(b))
		}
		return b, nil
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, fmt.Errorf("create data dir: %w", err)
	}
	master := make([]byte, 32)
	if _, err := io.ReadFull(rand.Reader, master); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}

	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if os.IsExist(err) {
		// Created concurrently since our ReadFile above; read the winner's key.
		b, rerr := os.ReadFile(path)
		if rerr != nil {
			return nil, fmt.Errorf("read master key after race: %w", rerr)
		}
		if len(b) != 32 {
			return nil, fmt.Errorf("master key file %s is corrupt (%d bytes, want 32)", path, len(b))
		}
		return b, nil
	}
	if err != nil {
		return nil, fmt.Errorf("create master key: %w", err)
	}
	defer f.Close()
	if _, err := f.Write(master); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	return master, nil
}
