package extension

import "github.com/matt0x6f/irc-client/internal/events"

// Host is the seam each runtime implements so the shared core can drive it
// without knowing whether it backs an in-process script or a subprocess plugin.
type Host interface {
	// Kind reports which runtime this host backs.
	Kind() Kind
	// Deliver dispatches an event to the extension identified by id.
	Deliver(id ID, ev events.Event) error
}
