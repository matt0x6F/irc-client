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

func TestClientIRCActions(t *testing.T) {
	var calls []string
	c := NewClient(nil, nil, nil, WithIRCActions(
		func(network, target, message string) { calls = append(calls, "notice:"+network+":"+target+":"+message) },
		func(network, target, message string) { calls = append(calls, "action:"+network+":"+target+":"+message) },
		func(network, channel, key string) { calls = append(calls, "join:"+network+":"+channel+":"+key) },
		func(network, channel, reason string) { calls = append(calls, "part:"+network+":"+channel+":"+reason) },
		func(network, nick string) { calls = append(calls, "nick:"+network+":"+nick) },
		func(network, message string) { calls = append(calls, "away:"+network+":"+message) },
	))
	n := c.Network("libera")
	n.Notice("#go", "heads up")
	n.User("alice").Notice("hello")
	n.Action("#go", "waves")
	n.Join("#go")
	n.JoinWithKey("#private", "secret")
	n.Part("#go", "bye")
	n.ChangeNick("Matt2")
	n.SetAway("lunch")
	n.ClearAway()

	want := []string{
		"notice:libera:#go:heads up", "notice:libera:alice:hello", "action:libera:#go:waves",
		"join:libera:#go:", "join:libera:#private:secret", "part:libera:#go:bye",
		"nick:libera:Matt2", "away:libera:lunch", "away:libera:",
	}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v; want %v", calls, want)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls[%d] = %q; want %q", i, calls[i], want[i])
		}
	}
}

func TestClientIRCActionsAreNilSafe(t *testing.T) {
	n := NewClient(nil, nil, nil).Network("missing")
	n.Notice("alice", "hi")
	n.User("alice").Notice("hi")
	n.Action("#go", "waves")
	n.Join("#go")
	n.JoinWithKey("#go", "key")
	n.Part("#go", "bye")
	n.ChangeNick("new")
	n.SetAway("away")
	n.ClearAway()
}
