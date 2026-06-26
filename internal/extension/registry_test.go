package extension

import (
	"testing"

	"github.com/matt0x6f/irc-client/internal/events"
)

// nopHost is a minimal Host used where delivery behavior is irrelevant.
type nopHost struct{}

func (nopHost) Kind() Kind                           { return KindScript }
func (nopHost) Deliver(id ID, ev events.Event) error { return nil }

func TestRegistryRegisterGetList(t *testing.T) {
	r := NewRegistry()
	ext := &Extension{ID: "greeter", Name: "Greeter", Kind: KindScript, Enabled: true, Status: StatusLoaded}
	r.Register(ext, nopHost{}, []string{"privmsg"})

	got, ok := r.Get("greeter")
	if !ok || got.ID != "greeter" || got.Name != "Greeter" {
		t.Fatalf("Get(greeter) = %+v, %v", got, ok)
	}
	if list := r.List(); len(list) != 1 || list[0].ID != "greeter" {
		t.Fatalf("List() = %+v; want one entry greeter", list)
	}
	if _, ok := r.Get("nope"); ok {
		t.Fatalf("Get(nope) should be false")
	}
}

func TestRegistrySetEnabledAndStatus(t *testing.T) {
	r := NewRegistry()
	ext := &Extension{ID: "x", Kind: KindScript, Enabled: true, Status: StatusLoaded}
	r.Register(ext, nopHost{}, nil)

	r.SetEnabled("x", false)
	got, _ := r.Get("x")
	if got.Enabled || got.Status != StatusDisabled {
		t.Fatalf("after disable: enabled=%v status=%v; want false/disabled", got.Enabled, got.Status)
	}

	r.SetEnabled("x", true)
	got, _ = r.Get("x")
	if !got.Enabled || got.Status != StatusLoaded {
		t.Fatalf("after enable: enabled=%v status=%v; want true/loaded", got.Enabled, got.Status)
	}

	r.SetStatus("x", StatusError, "boom")
	got, _ = r.Get("x")
	if got.Status != StatusError || got.Err != "boom" {
		t.Fatalf("after SetStatus: status=%v err=%q; want error/boom", got.Status, got.Err)
	}
}

func TestRegistryRecipientsFiltersByTypeAndEnabled(t *testing.T) {
	r := NewRegistry()
	r.Register(&Extension{ID: "a", Enabled: true, Status: StatusLoaded}, nopHost{}, []string{"privmsg"})
	r.Register(&Extension{ID: "b", Enabled: true, Status: StatusLoaded}, nopHost{}, []string{"join"})
	r.Register(&Extension{ID: "c", Enabled: false, Status: StatusDisabled}, nopHost{}, []string{"privmsg"})

	rcpts := r.recipients("privmsg")
	if len(rcpts) != 1 || rcpts[0].id != "a" {
		t.Fatalf("recipients(privmsg) = %+v; want only a", rcpts)
	}
}

func TestRegistryRegisterReplacesEntry(t *testing.T) {
	r := NewRegistry()
	ext := &Extension{ID: "x", Enabled: true, Status: StatusLoaded}
	r.Register(ext, nopHost{}, []string{"a"})
	// Re-register the same id with a different subscription set.
	r.Register(ext, nopHost{}, []string{"b"})

	if rc := r.recipients("a"); len(rc) != 0 {
		t.Fatalf("after replace, recipients(a) = %v; want empty (subscriptions replaced, not merged)", rc)
	}
	if rc := r.recipients("b"); len(rc) != 1 || rc[0].id != "x" {
		t.Fatalf("after replace, recipients(b) = %v; want [x]", rc)
	}
}
