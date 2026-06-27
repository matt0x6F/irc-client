package script

import (
	"fmt"
	"sync"
	"time"

	"github.com/matt0x6f/irc-client/internal/extension"
	"github.com/matt0x6f/irc-client/internal/logger"
)

// timerEntry holds a single registered timer and a stop function so we can
// cancel it without holding any other lock.
type timerEntry struct {
	stop func()
}

// scheduler is a per-script timer registry. It is embedded in Manager and has
// its own mutex so timer operations never need to hold m.mu.
//
// Lock discipline:
//
//	scheduler.mu: held ONLY to read/write the timers map. Never held while a
//	timer callback runs (time.AfterFunc goroutines call out without the lock).
//	m.mu: held by dispatch / loadDir / unload as usual. scheduler.mu is
//	always acquired separately; the two are never held simultaneously.
//	l.mu: taken inside runScriptFn, called from timer goroutines. Since we
//	do not hold scheduler.mu when calling runScriptFn, and dispatch also only
//	holds l.mu while calling into the script, there is no cycle.
type scheduler struct {
	mu     sync.Mutex
	timers map[extension.ID][]*timerEntry
}

func newScheduler() *scheduler {
	return &scheduler{timers: make(map[extension.ID][]*timerEntry)}
}

// addTimer registers a timerEntry under id and returns it.
func (s *scheduler) addTimer(id extension.ID, stop func()) *timerEntry {
	e := &timerEntry{stop: stop}
	s.mu.Lock()
	s.timers[id] = append(s.timers[id], e)
	s.mu.Unlock()
	return e
}

// stopTimers cancels all timers for id and removes the entry. Safe to call
// concurrently; idempotent.
func (s *scheduler) stopTimers(id extension.ID) {
	s.mu.Lock()
	entries := s.timers[id]
	delete(s.timers, id)
	s.mu.Unlock()
	// Call stop outside the lock — the stop funcs are cheap (timer.Stop) and
	// must not call back into the scheduler.
	for _, e := range entries {
		e.stop()
	}
}

// stopAll cancels every script's timers. Used on Manager shutdown.
func (s *scheduler) stopAll() {
	s.mu.Lock()
	// Collect all stop funcs under lock, then call them outside it (mirrors stopTimers).
	var stops []func()
	for id, entries := range s.timers {
		for _, e := range entries {
			stops = append(stops, e.stop)
		}
		delete(s.timers, id)
	}
	s.mu.Unlock()
	for _, stop := range stops {
		stop()
	}
}

// runScriptFn looks up the loaded script for id, takes its per-script mutex,
// recovers any panic, and calls fn. This mirrors the serialisation that
// dispatch() applies to event handlers, ensuring timer ticks and event
// dispatch are mutually exclusive per script. A panic is returned as an error
// so the watchdog can count it.
func (m *Manager) runScriptFn(id extension.ID, fn func()) (err error) {
	m.mu.RLock()
	l, ok := m.scripts[id]
	m.mu.RUnlock()
	if !ok {
		// Script was unloaded; nothing to do.
		return nil
	}
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("script %s timer panic: %v", id, r)
		}
	}()
	l.mu.Lock()
	defer l.mu.Unlock()
	fn()
	return nil
}

// scheduleEvery returns the closure the Client passes to c.Every. Each call
// registers a self-rescheduling time.AfterFunc whose tick runs through the
// watchdog + per-script mutex. An invalid duration is logged and ignored.
func (m *Manager) scheduleEvery(id extension.ID) func(interval string, fn func()) {
	return func(interval string, fn func()) {
		d, err := time.ParseDuration(interval)
		if err != nil {
			logger.Log.Warn().Str("script", string(id)).Str("interval", interval).Msg("c.Every: invalid duration, ignoring")
			return
		}

		// stopped is a flag set when stopTimers is called; it breaks the
		// re-arming cycle without needing a channel. Protected by the closure
		// (written under scheduler.mu via the stop func, read inside the timer
		// goroutine which checks it before re-arming).
		var (
			mu      sync.Mutex
			stopped bool
			t       *time.Timer
		)

		var tick func()
		tick = func() {
			// Run the fn through the watchdog. If the script has been unloaded,
			// runScriptFn returns nil quickly; the watchdog is a no-op overhead.
			_ = m.watchdog.run(id, func() error { return m.runScriptFn(id, fn) })

			// Re-arm only if not stopped.
			mu.Lock()
			if !stopped {
				t = time.AfterFunc(d, tick)
			}
			mu.Unlock()
		}

		mu.Lock()
		t = time.AfterFunc(d, tick)
		mu.Unlock()

		m.sched.addTimer(id, func() {
			mu.Lock()
			stopped = true
			if t != nil {
				t.Stop()
			}
			mu.Unlock()
		})
	}
}

// scheduleAfter returns the closure the Client passes to c.After. The fn runs
// exactly once after delay through the watchdog + per-script mutex.
func (m *Manager) scheduleAfter(id extension.ID) func(delay string, fn func()) {
	return func(delay string, fn func()) {
		d, err := time.ParseDuration(delay)
		if err != nil {
			logger.Log.Warn().Str("script", string(id)).Str("delay", delay).Msg("c.After: invalid duration, ignoring")
			return
		}

		var (
			mu      sync.Mutex
			stopped bool
			t       *time.Timer
		)

		t = time.AfterFunc(d, func() {
			mu.Lock()
			if stopped {
				mu.Unlock()
				return
			}
			mu.Unlock()
			// Deliberate: we release mu before watchdog.run so a concurrent stopTimers
			// never blocks on an in-flight tick. A stop racing in after this check lets
			// one fn run, which is safe — runScriptFn re-validates the script is still
			// loaded under m.mu and no-ops otherwise. (Same benign window as Every.)
			_ = m.watchdog.run(id, func() error { return m.runScriptFn(id, fn) })
		})

		m.sched.addTimer(id, func() {
			mu.Lock()
			stopped = true
			if t != nil {
				t.Stop()
			}
			mu.Unlock()
		})
	}
}
