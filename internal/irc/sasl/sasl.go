// Package sasl implements pluggable SASL mechanisms driven by the vendored
// ircevent fork's SASLMechanism hook, so authentication completes before
// registration for every mechanism.
package sasl

import "fmt"

// Mechanism is structurally identical to ircevent.SASLMechanism; keeping a local
// interface avoids importing the fork into pure unit tests.
type Mechanism interface {
	Name() string
	Respond(challenge []byte) (response []byte, err error)
}

// ForNetwork selects a mechanism by name. An empty name defaults to PLAIN.
func ForNetwork(mech, username, password string) (Mechanism, error) {
	switch mech {
	case "", "PLAIN":
		return NewPlain(username, password), nil
	case "EXTERNAL":
		return NewExternal(), nil
	case "SCRAM-SHA-256", "SCRAM-SHA-512":
		return NewScram(mech, username, password)
	default:
		return nil, fmt.Errorf("unsupported SASL mechanism %q", mech)
	}
}
