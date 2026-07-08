//go:build !darwin && !linux

package security

// machineID has no cheap, dependency-free source on other platforms (e.g.
// Windows), so it returns "". The encrypted-file store still works; it just
// omits the extra machine-binding layer of the key derivation there.
func machineID() string { return "" }
