package security

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
)

// FileBackend implements SecretBackend by storing secrets in an AES-256-GCM
// encrypted file. Unlike the OS keychain, access depends only on the file and
// the encryption key — never on the app's code signature — so secrets survive
// app updates even on ad-hoc-signed macOS builds (whose per-binary designated
// requirement changes every release and locks the keychain out).
//
// Security posture: the secrets are encrypted at rest, but the key must be
// available non-interactively (see deriveFileKey), so a process running as the
// same user can reproduce it. This is the unavoidable ceiling for an app with
// no stable signed identity; it is still strictly stronger than the plaintext
// database-column fallback it replaces.
type FileBackend struct {
	path string
	key  []byte
	mu   sync.Mutex
}

// NewFileBackendWithKey builds a FileBackend over an explicit 32-byte AES-256
// key and file path. Used directly by tests and by NewFileBackend, which
// derives the key.
func NewFileBackendWithKey(path string, key []byte) *FileBackend {
	return &FileBackend{path: path, key: key}
}

// Set stores value under key, replacing any existing entry.
func (f *FileBackend) Set(key, value string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	m[key] = value
	return f.save(m)
}

// Get returns the value stored under key. A missing key returns "" with no
// error, matching the SecretBackend contract.
func (f *FileBackend) Get(key string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return "", err
	}
	return m[key], nil
}

// Delete removes the entry for key. A missing key is a no-op.
func (f *FileBackend) Delete(key string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	m, err := f.load()
	if err != nil {
		return err
	}
	if _, ok := m[key]; !ok {
		return nil
	}
	delete(m, key)
	return f.save(m)
}

// load reads and decrypts the secrets map. A missing file is an empty map.
func (f *FileBackend) load() (map[string]string, error) {
	raw, err := os.ReadFile(f.path)
	if os.IsNotExist(err) {
		return map[string]string{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read credentials file: %w", err)
	}
	if len(raw) == 0 {
		return map[string]string{}, nil
	}

	gcm, err := f.gcm()
	if err != nil {
		return nil, err
	}
	if len(raw) < gcm.NonceSize() {
		return nil, fmt.Errorf("credentials file truncated")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plaintext, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, fmt.Errorf("decrypt credentials file: %w", err)
	}

	m := map[string]string{}
	if err := json.Unmarshal(plaintext, &m); err != nil {
		return nil, fmt.Errorf("parse credentials file: %w", err)
	}
	return m, nil
}

// save encrypts and atomically writes the secrets map with 0600 permissions.
func (f *FileBackend) save(m map[string]string) error {
	plaintext, err := json.Marshal(m)
	if err != nil {
		return fmt.Errorf("encode credentials: %w", err)
	}

	gcm, err := f.gcm()
	if err != nil {
		return err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return fmt.Errorf("generate nonce: %w", err)
	}
	sealed := gcm.Seal(nonce, nonce, plaintext, nil)

	if err := os.MkdirAll(filepath.Dir(f.path), 0o700); err != nil {
		return fmt.Errorf("create credentials dir: %w", err)
	}
	// Write to a temp file and rename so a crash mid-write can't corrupt the
	// existing store.
	tmp, err := os.CreateTemp(filepath.Dir(f.path), ".credentials-*.tmp")
	if err != nil {
		return fmt.Errorf("create temp credentials file: %w", err)
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return fmt.Errorf("chmod temp credentials file: %w", err)
	}
	if _, err := tmp.Write(sealed); err != nil {
		tmp.Close()
		return fmt.Errorf("write temp credentials file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("close temp credentials file: %w", err)
	}
	if err := os.Rename(tmpName, f.path); err != nil {
		return fmt.Errorf("replace credentials file: %w", err)
	}
	return nil
}

func (f *FileBackend) gcm() (cipher.AEAD, error) {
	block, err := aes.NewCipher(f.key)
	if err != nil {
		return nil, fmt.Errorf("init cipher: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("init gcm: %w", err)
	}
	return gcm, nil
}
