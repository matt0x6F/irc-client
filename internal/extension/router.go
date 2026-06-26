package extension

import "github.com/matt0x6f/irc-client/internal/events"

// Router subscribes to the event bus and fans each event out to the enabled
// extensions that subscribed to its type, via their Host. Plugins and scripts
// plug in as Hosts; the Router stays runtime-agnostic.
type Router struct {
	reg *Registry
}

// NewRouter creates a Router backed by the given registry.
func NewRouter(reg *Registry) *Router {
	return &Router{reg: reg}
}

// OnEvent implements events.Subscriber. It delivers ev to every enabled
// extension subscribed to ev.Type; a Deliver error marks that extension errored
// but never blocks delivery to the others.
func (rt *Router) OnEvent(ev events.Event) {
	for _, d := range rt.reg.recipients(ev.Type) {
		if err := d.host.Deliver(d.id, ev); err != nil {
			rt.reg.SetStatus(d.id, StatusError, err.Error())
		}
	}
}

// Attach subscribes the Router to every event on the bus (the "*" wildcard);
// per-type filtering is done from the registry's subscriptions.
func (rt *Router) Attach(bus *events.EventBus) {
	bus.Subscribe("*", rt)
}
