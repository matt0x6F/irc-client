//go:build darwin

package security

import (
	"os/exec"
	"regexp"
)

var ioPlatformUUIDRE = regexp.MustCompile(`"IOPlatformUUID" = "([^"]+)"`)

// machineID returns the macOS hardware UUID (stable across reboots and app
// updates), or "" if it can't be read.
func machineID() string {
	out, err := exec.Command("ioreg", "-rd1", "-c", "IOPlatformExpertDevice").Output()
	if err != nil {
		return ""
	}
	if m := ioPlatformUUIDRE.FindSubmatch(out); m != nil {
		return string(m[1])
	}
	return ""
}
