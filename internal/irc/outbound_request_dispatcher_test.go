package irc

import (
	"sync"
	"testing"
	"time"
)

func TestOutboundRequestDispatcherPacesBurstAndPreservesOrder(t *testing.T) {
	permits := make(chan struct{})
	d := newOutboundRequestDispatcher(func() { <-permits })
	t.Cleanup(d.stopNow)

	const count = 100
	var mu sync.Mutex
	got := make([]int, 0, count)
	for i := 0; i < count; i++ {
		i := i
		if !d.enqueue("test", func() error {
			mu.Lock()
			got = append(got, i)
			mu.Unlock()
			return nil
		}) {
			t.Fatalf("enqueue %d rejected", i)
		}
	}

	// A queued registration burst must not become an immediate wire burst.
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	if len(got) != 0 {
		t.Fatalf("ran %d requests without rate-limit permits", len(got))
	}
	mu.Unlock()

	for i := 0; i < count; i++ {
		permits <- struct{}{}
		deadline := time.Now().Add(time.Second)
		for {
			mu.Lock()
			n := len(got)
			mu.Unlock()
			if n == i+1 {
				break
			}
			if time.Now().After(deadline) {
				t.Fatalf("request %d did not run after permit", i)
			}
			time.Sleep(time.Millisecond)
		}
	}

	mu.Lock()
	defer mu.Unlock()
	for i, value := range got {
		if value != i {
			t.Fatalf("request %d ran as %d", i, value)
		}
	}
}

func TestOutboundRequestDispatcherDropsQueuedWorkOnStop(t *testing.T) {
	enteredWait := make(chan struct{})
	releaseWait := make(chan struct{})
	d := newOutboundRequestDispatcher(func() {
		close(enteredWait)
		<-releaseWait
	})

	ran := make(chan struct{}, 1)
	if !d.enqueue("test", func() error {
		ran <- struct{}{}
		return nil
	}) {
		t.Fatal("enqueue rejected before stop")
	}
	<-enteredWait
	d.stopNow()
	close(releaseWait)
	select {
	case <-d.done:
	case <-time.After(time.Second):
		t.Fatal("dispatcher did not stop")
	}
	select {
	case <-ran:
		t.Fatal("request ran after stop")
	default:
	}
	if d.enqueue("late", func() error { return nil }) {
		t.Fatal("enqueue accepted after stop")
	}
}
