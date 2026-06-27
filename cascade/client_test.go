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
