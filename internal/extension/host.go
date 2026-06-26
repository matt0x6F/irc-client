package extension

import "github.com/matt0x6f/irc-client/internal/events"

// Host is the seam each runtime implements so the shared core can drive it
// without knowing whether it backs an in-process script or a subprocess plugin.
type Host interface {
	// Kind reports which runtime this host backs.
	Kind() Kind
	// Deliver dispatches an event to the extension identified by id.
	// ev is shared with the other hosts receiving the same event — implementations
	// MUST treat it as read-only and must not mutate ev or ev.Data.
	Deliver(id ID, ev events.Event) error
}
