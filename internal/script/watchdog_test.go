package script

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/extension"
)

type disableSpy struct {
	mu    sync.Mutex
	calls []extension.ID
}

func (d *disableSpy) fn(id extension.ID, _ string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.calls = append(d.calls, id)
}
func (d *disableSpy) count() int { d.mu.Lock(); defer d.mu.Unlock(); return len(d.calls) }

func TestWatchdogDeadlineDisables(t *testing.T) {
	spy := &disableSpy{}
	w := newWatchdog(20*time.Millisecond, 3, spy.fn)

	block := make(chan struct{})
	err := w.run("hang", func() error { <-block; return nil }) // blocks past the deadline
	if err == nil {
		t.Fatalf("expected a deadline error")
	}
	if spy.count() != 1 || spy.calls[0] != "hang" {
		t.Fatalf("expected disable('hang'); got %v", spy.calls)
	}
	close(block) // release the goroutine cleanly (no leak/CPU in the test)
}

func TestWatchdogDisablesAfterNStrikes(t *testing.T) {
	spy := &disableSpy{}
	w := newWatchdog(time.Second, 3, spy.fn)
	boom := func() error { return errors.New("boom") }

	w.run("p", boom)
	w.run("p", boom)
	if spy.count() != 0 {
		t.Fatalf("must not disable before maxStrikes; got %d", spy.count())
	}
	w.run("p", boom) // 3rd strike
	if spy.count() != 1 || spy.calls[0] != "p" {
		t.Fatalf("expected disable after 3 strikes; got %v", spy.calls)
	}
}

func TestWatchdogSuccessClearsStrikes(t *testing.T) {
	spy := &disableSpy{}
	w := newWatchdog(time.Second, 2, spy.fn)
	boom := func() error { return errors.New("boom") }
	ok := func() error { return nil }

	w.run("p", boom) // strike 1
	w.run("p", ok)   // clears
	w.run("p", boom) // strike 1 again
	if spy.count() != 0 {
		t.Fatalf("a success between panics must reset strikes; got %d disables", spy.count())
	}
}

func TestWatchdogPanicCountsAsStrike(t *testing.T) {
	spy := &disableSpy{}
	w := newWatchdog(time.Second, 1, spy.fn)
	err := w.run("p", func() error { panic("kaboom") })
	if err == nil || spy.count() != 1 {
		t.Fatalf("a panicking dispatch must be recovered and count as a strike; err=%v disables=%d", err, spy.count())
	}
}
