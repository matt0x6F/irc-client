package extension

import (
	"errors"
	"sync"
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
)

// recordingHost records every Deliver call and can be told to fail.
type recordingHost struct {
	mu       sync.Mutex
	got      []events.Event
	gotIDs   []ID
	failWith error
}

func (h *recordingHost) Kind() Kind { return KindScript }
func (h *recordingHost) Deliver(id ID, ev events.Event) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.gotIDs = append(h.gotIDs, id)
	h.got = append(h.got, ev)
	return h.failWith
}

func TestRouterDeliversToSubscribedEnabled(t *testing.T) {
	r := NewRegistry()
	h := &recordingHost{}
	r.Register(&Extension{ID: "a", Enabled: true, Status: StatusLoaded}, h, []string{"privmsg"})

	rt := NewRouter(r)
	rt.OnEvent(events.Event{Type: "privmsg", Data: map[string]interface{}{"text": "hi"}})

	if len(h.got) != 1 || h.gotIDs[0] != "a" || h.got[0].Type != "privmsg" {
		t.Fatalf("deliver = ids:%v evts:%v; want one privmsg to a", h.gotIDs, h.got)
	}
}

func TestRouterSkipsDisabledAndWrongType(t *testing.T) {
	r := NewRegistry()
	h := &recordingHost{}
	r.Register(&Extension{ID: "disabled", Enabled: false, Status: StatusDisabled}, h, []string{"privmsg"})
	r.Register(&Extension{ID: "wrongtype", Enabled: true, Status: StatusLoaded}, h, []string{"join"})

	rt := NewRouter(r)
	rt.OnEvent(events.Event{Type: "privmsg"})

	if len(h.got) != 0 {
		t.Fatalf("expected no deliveries (disabled + wrong-type); got %v", h.gotIDs)
	}
}

func TestRouterDeliverErrorSetsStatus(t *testing.T) {
	r := NewRegistry()
	h := &recordingHost{failWith: errors.New("kaboom")}
	r.Register(&Extension{ID: "a", Enabled: true, Status: StatusLoaded}, h, []string{"privmsg"})

	rt := NewRouter(r)
	rt.OnEvent(events.Event{Type: "privmsg"})

	got, _ := r.Get("a")
	if got.Status != StatusError || got.Err != "kaboom" {
		t.Fatalf("after Deliver error: status=%v err=%q; want error/kaboom", got.Status, got.Err)
	}
}

func TestRouterAttachReceivesBusEvents(t *testing.T) {
	r := NewRegistry()
	h := &recordingHost{}
	r.Register(&Extension{ID: "a", Enabled: true, Status: StatusLoaded}, h, []string{"privmsg"})

	bus := events.NewEventBus()
	rt := NewRouter(r)
	rt.Attach(bus)

	bus.EmitSync(events.Event{Type: "privmsg", Source: events.EventSourceIRC})

	if len(h.got) != 1 || h.gotIDs[0] != "a" {
		t.Fatalf("attached router delivery = %v; want one to a", h.gotIDs)
	}
}
