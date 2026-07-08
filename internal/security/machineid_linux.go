//go:build linux

package security

import (
	"os"
	"strings"
)

// machineID returns the systemd/D-Bus machine id (stable across app updates),
// or "" if it can't be read.
func machineID() string {
	for _, p := range []string{"/etc/machine-id", "/var/lib/dbus/machine-id"} {
		if b, err := os.ReadFile(p); err == nil {
			if id := strings.TrimSpace(string(b)); id != "" {
				return id
			}
		}
	}
	return ""
}
