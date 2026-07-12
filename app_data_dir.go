package main

import (
	"fmt"
	"os"
)

// ensurePrivateDir creates a user-data directory and tightens an existing one.
// Chat history, connection metadata, scripts, and encrypted credentials all live
// below this path, so directory traversal must be limited to the current user.
func ensurePrivateDir(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create private data directory: %w", err)
	}
	if err := os.Chmod(path, 0o700); err != nil {
		return fmt.Errorf("secure private data directory: %w", err)
	}
	return nil
}
