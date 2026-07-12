package cascade

import (
	"sync"
	"testing"
)

func TestClientNetworkSayAndTimers(t *testing.T) {
	var mu sync.Mutex
	var sends [][3]string
	var everies, afters []string

	c := NewClient(
		func(net, target, msg string) {
			mu.Lock()
			sends = append(sends, [3]string{net, target, msg})
			mu.Unlock()
		},
		func(interval string, fn func()) { everies = append(everies, interval); fn() },
		func(delay string, fn func()) { afters = append(afters, delay); fn() },
	)

	c.Network("libera").Say("#go", "hi")
	c.Every("5m", func() { c.Network("libera").Say("#go", "tick") })
	c.After("30s", func() {})

	mu.Lock()
	defer mu.Unlock()
	if len(sends) != 2 || sends[0] != [3]string{"libera", "#go", "hi"} || sends[1] != [3]string{"libera", "#go", "tick"} {
		t.Fatalf("sends = %v", sends)
	}
	if len(everies) != 1 || everies[0] != "5m" || len(afters) != 1 || afters[0] != "30s" {
		t.Fatalf("timers = %v %v", everies, afters)
	}
}

func TestClientNetworkQueries(t *testing.T) {
	c := NewClient(nil, nil, nil, WithNetworkQueries(
		func(network string) bool { return network == "libera" },
		func(network string) string {
			if network == "libera" {
				return "Matt"
			}
			return ""
		},
		func(network, nick string) bool { return network == "libera" && (nick == "Matt" || nick == "mAtT") },
		func(network, nick string) UserStatus {
			if network == "libera" && nick == "Alice" {
				return UserStatus{Known: true, Away: true, AwayMessage: "lunch", Account: "alice_account"}
			}
			return UserStatus{}
		},
	))

	net := c.Network("libera")
	if net.Name() != "libera" || !net.IsConnected() || net.Nick() != "Matt" {
		t.Fatalf("network queries = %q %v %q", net.Name(), net.IsConnected(), net.Nick())
	}
	if !net.IsMe("mAtT") || !net.Self().IsSelf() || net.Self().Nick() != "Matt" {
		t.Fatalf("self identity was not delegated to the host")
	}
	alice := net.User("Alice")
	if !alice.Known() || !alice.IsAway() || alice.Status().AwayMessage != "lunch" || alice.Status().Account != "alice_account" {
		t.Fatalf("alice status = %+v", alice.Status())
	}
	if net.User("unknown").Known() || net.User("unknown").IsAway() {
		t.Fatal("unknown user reported known or away")
	}
}

func TestClientUnknownQueriesAreNilSafe(t *testing.T) {
	c := NewClient(nil, nil, nil)
	net := c.Network("missing")
	if net.IsConnected() || net.Nick() != "" || net.IsMe("anyone") {
		t.Fatalf("unbound network returned non-zero queries")
	}
	u := net.User("alice")
	if u.Known() || u.IsAway() || u.IsSelf() || u.Status() != (UserStatus{}) {
		t.Fatalf("unbound user status = %+v", u.Status())
	}
}
