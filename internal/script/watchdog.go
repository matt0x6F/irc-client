package script

import (
	"fmt"
	"sync"
	"time"

	"github.com/matt0x6f/irc-client/internal/extension"
)

const (
	defaultDispatchDeadline = 2 * time.Second
	defaultMaxStrikes       = 3
)

// watchdog bounds a script's dispatch. A dispatch exceeding the deadline (a hang
// or infinite loop) disables the script immediately; consecutive errors/panics
// disable it after maxStrikes; a success clears the count.
//
// HONEST LIMIT: Go cannot preempt a goroutine, so on a deadline overrun the
// dispatch goroutine LEAKS — we disable the script (stopping further dispatch)
// but cannot reclaim the spinning goroutine. We contain blast radius, not kill.
type watchdog struct {
	deadline   time.Duration
	maxStrikes int
	disable    func(id extension.ID, reason string)

	mu      sync.Mutex
	strikes map[extension.ID]int
}

func newWatchdog(deadline time.Duration, maxStrikes int, disable func(extension.ID, string)) *watchdog {
	return &watchdog{
		deadline:   deadline,
		maxStrikes: maxStrikes,
		disable:    disable,
		strikes:    make(map[extension.ID]int),
	}
}

// run executes dispatch under the deadline + strike policy and returns the
// dispatch error (or a deadline error). Panics inside dispatch are recovered and
// returned as errors (counted as strikes).
func (w *watchdog) run(id extension.ID, dispatch func() error) error {
	done := make(chan error, 1) // buffered: a late goroutine never blocks on send
	go func() {
		defer func() {
			if r := recover(); r != nil {
				done <- fmt.Errorf("panic: %v", r)
			}
		}()
		done <- dispatch()
	}()

	select {
	case err := <-done:
		w.record(id, err)
		return err
	case <-time.After(w.deadline):
		w.disable(id, fmt.Sprintf("dispatch exceeded %s deadline", w.deadline))
		return fmt.Errorf("script %s: dispatch exceeded %s deadline", id, w.deadline)
	}
}

// record updates the strike count; disable is called (outside the lock) once the
// count reaches maxStrikes.
func (w *watchdog) record(id extension.ID, err error) {
	if err == nil {
		w.mu.Lock()
		delete(w.strikes, id)
		w.mu.Unlock()
		return
	}
	w.mu.Lock()
	w.strikes[id]++
	n := w.strikes[id]
	hit := n >= w.maxStrikes
	if hit {
		delete(w.strikes, id)
	}
	w.mu.Unlock()
	// disable is called OUTSIDE the lock to avoid holding mu during an
	// arbitrary callback (prevents inversion if disable tries to acquire mu).
	if hit {
		w.disable(id, fmt.Sprintf("disabled after %d consecutive errors: %v", n, err))
	}
}
