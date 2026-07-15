package irc

import (
	"sync"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
)

func TestCallbackDispatcherEnqueueDoesNotWaitForSlowWork(t *testing.T) {
	d := newCallbackDispatcher()
	started := make(chan struct{})
	release := make(chan struct{})
	if !d.enqueue("blocked", func() {
		close(started)
		<-release
	}) {
		t.Fatal("initial enqueue rejected")
	}
	<-started

	done := make(chan struct{})
	go func() {
		for i := 0; i < 10_000; i++ {
			if !d.enqueue("burst", func() {}) {
				t.Errorf("enqueue %d rejected before shutdown", i)
				break
			}
		}
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("enqueue blocked behind a slow application callback")
	}
	close(release)
	d.stopAfterDrain()
	select {
	case <-d.done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not drain and stop")
	}
}

func TestCallbackDispatcherPreservesOrder(t *testing.T) {
	d := newCallbackDispatcher()
	var mu sync.Mutex
	got := make([]int, 0, 1_000)
	for i := 0; i < 1_000; i++ {
		i := i
		if !d.enqueue("ordered", func() {
			mu.Lock()
			got = append(got, i)
			mu.Unlock()
		}) {
			t.Fatalf("enqueue %d rejected", i)
		}
	}
	d.stopAfterDrain()
	<-d.done

	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1_000 {
		t.Fatalf("delivered %d callbacks, want 1000", len(got))
	}
	for i, value := range got {
		if value != i {
			t.Fatalf("callback %d delivered value %d", i, value)
		}
	}
	if d.enqueue("late", func() {}) {
		t.Fatal("enqueue accepted after shutdown")
	}
}

func TestIRCGoCallbackReturnsWhileApplicationWorkerIsBlocked(t *testing.T) {
	d := newCallbackDispatcher()
	conn := &ircevent.Connection{}
	c := &IRCClient{conn: conn, callbacks: d}
	started := make(chan struct{})
	release := make(chan struct{})
	c.addCallback("353", func(ircmsg.Message) {
		select {
		case <-started:
		default:
			close(started)
		}
		<-release
	})

	message := ircmsg.Message{Command: "353"}
	conn.HandleMessage(message)
	<-started

	returned := make(chan struct{})
	go func() {
		for i := 0; i < 1_000; i++ {
			conn.HandleMessage(message)
		}
		close(returned)
	}()
	select {
	case <-returned:
	case <-time.After(250 * time.Millisecond):
		t.Fatal("irc-go message handling waited for application callback work")
	}

	close(release)
	d.stopAfterDrain()
	select {
	case <-d.done:
	case <-time.After(2 * time.Second):
		t.Fatal("dispatcher did not drain after releasing callback")
	}
}
