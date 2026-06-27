package script

// Scheduler tests — deterministic via poll-with-timeout (no fixed sleeps).
//
// Pattern: schedule a very-short timer (1ms or 5ms) that records a side effect
// (via the fake host's Send), then poll up to ~2s with a tiny check interval
// until the effect is observed. This avoids asserting tight wall-clock timing
// while remaining deterministic under a generous margin.

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/extension"
)

// pollUntil returns true as soon as cond() returns true, or false after timeout.
func pollUntil(timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}

// writeTimerScript writes a script into dir/<name>/main.go that calls either
// c.After or c.Every with a very short delay, sending a message via
// c.Network("net").Say(target, text) so we can detect the tick.
func writeTimerScript(t *testing.T, dir, name, setupBody string) string {
	t.Helper()
	d := filepath.Join(dir, name)
	if err := os.MkdirAll(d, 0o755); err != nil {
		t.Fatal(err)
	}
	src := `package main
import "github.com/matt0x6f/irc-client/cascade"
func Setup(c *cascade.Client) {
` + setupBody + `
}
`
	if err := os.WriteFile(filepath.Join(d, "s.go"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	return d
}

// timerTestHost builds a Host where "net" resolves to network ID 1.
func timerTestHost(fs *fakeSender) Host {
	return Host{
		Send:     fs.send,
		SelfNick: noSelf,
		ResolveNetwork: func(name string) (int64, bool) {
			if name == "net" {
				return 1, true
			}
			return 0, false
		},
	}
}

// hasSent reports (under the fake sender's lock) whether any recorded send
// matches the given message string.
func hasSent(fs *fakeSender, msg string) bool {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	for _, s := range fs.sent {
		if s.message == msg {
			return true
		}
	}
	return false
}

// countSent returns the number of recorded sends matching msg.
func countSent(fs *fakeSender, msg string) int {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	n := 0
	for _, s := range fs.sent {
		if s.message == msg {
			n++
		}
	}
	return n
}

// TestSchedulerAfterFiresOnce verifies that c.After schedules fn to fire
// exactly once, and that after unload it fires no additional times.
func TestSchedulerAfterFiresOnce(t *testing.T) {
	dir := t.TempDir()
	writeTimerScript(t, dir, "aftersay", `c.After("1ms", func(){ c.Network("net").Say("#ch", "after-fired") })`)

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, timerTestHost(fs))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}

	// Poll up to 2s for the after-tick to deliver.
	if !pollUntil(2*time.Second, func() bool { return hasSent(fs, "after-fired") }) {
		t.Fatal("c.After did not fire within 2s")
	}

	// Snapshot the count after the first fire and wait a bit to confirm it
	// does NOT fire again (it is one-shot).
	snap := countSent(fs, "after-fired")
	time.Sleep(50 * time.Millisecond)
	if n := countSent(fs, "after-fired"); n != snap {
		t.Fatalf("c.After fired more than once: got %d sends", n)
	}

	// Unload and confirm no further fires.
	m.unload(extension.ID("aftersay"))
	time.Sleep(50 * time.Millisecond)
	final := countSent(fs, "after-fired")
	if final != snap {
		t.Fatalf("c.After fired after unload: was %d, now %d", snap, final)
	}
}

// TestSchedulerEveryFires verifies that c.Every fires at least once, and that
// after unload no further ticks are delivered.
func TestSchedulerEveryFires(t *testing.T) {
	dir := t.TempDir()
	writeTimerScript(t, dir, "everysay", `c.Every("5ms", func(){ c.Network("net").Say("#ch", "every-tick") })`)

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, timerTestHost(fs))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}

	// Poll up to 2s for at least one tick.
	if !pollUntil(2*time.Second, func() bool { return countSent(fs, "every-tick") >= 1 }) {
		t.Fatal("c.Every did not fire within 2s")
	}

	// Unload; grab count immediately after.
	m.unload(extension.ID("everysay"))
	afterUnload := countSent(fs, "every-tick")

	// Wait a generous settle window — any in-flight tick that was already
	// scheduled before stopTimers fires is the only expected addition. After
	// that window there must be no further ticks.
	time.Sleep(50 * time.Millisecond)
	final := countSent(fs, "every-tick")

	// We allow at most one extra tick that was in-flight at unload time.
	// What we must NOT see is unbounded continued firing.
	// Wait another window to be sure.
	time.Sleep(50 * time.Millisecond)
	final2 := countSent(fs, "every-tick")
	if final2 != final {
		t.Fatalf("c.Every continued firing after unload+settle: %d → %d ticks", final, final2)
	}
	_ = afterUnload // informational
}

// TestSchedulerEveryStopsOnReload verifies that old timers are cancelled when
// the script is reloaded, so ticks don't accumulate from both versions.
func TestSchedulerEveryStopsOnReload(t *testing.T) {
	dir := t.TempDir()
	sdir := writeTimerScript(t, dir, "reloadsay",
		`c.Every("5ms", func(){ c.Network("net").Say("#ch", "v1-tick") })`)

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, timerTestHost(fs))
	if err := m.LoadAll(); err != nil {
		t.Fatal(err)
	}

	// Wait for at least one v1 tick.
	if !pollUntil(2*time.Second, func() bool { return hasSent(fs, "v1-tick") }) {
		t.Fatal("v1 Every did not fire within 2s")
	}

	// Rewrite the script to v2 and reload.
	src2 := `package main
import "github.com/matt0x6f/irc-client/cascade"
func Setup(c *cascade.Client) {
	c.Every("5ms", func(){ c.Network("net").Say("#ch", "v2-tick") })
}
`
	if err := os.WriteFile(filepath.Join(sdir, "s.go"), []byte(src2), 0o644); err != nil {
		t.Fatal(err)
	}
	m.reload(sdir)

	// Wait for v2 ticks.
	if !pollUntil(2*time.Second, func() bool { return hasSent(fs, "v2-tick") }) {
		t.Fatal("v2 Every did not fire within 2s after reload")
	}

	// Snapshot v1 count; after a settle window it must not grow.
	snapV1 := countSent(fs, "v1-tick")
	time.Sleep(50 * time.Millisecond)
	if n := countSent(fs, "v1-tick"); n != snapV1 {
		t.Fatalf("v1 timer continued firing after reload: %d → %d", snapV1, n)
	}

	// Clean up.
	m.unload(extension.ID("reloadsay"))
}

// TestSchedulerInvalidDurationNoops verifies that an invalid duration string
// passed to c.After / c.Every is silently ignored (no panic, no crash).
func TestSchedulerInvalidDurationNoops(t *testing.T) {
	dir := t.TempDir()
	writeTimerScript(t, dir, "baddur", `
		c.After("not-a-duration", func(){ c.Network("net").Say("#ch", "bad-after") })
		c.Every("also-bad", func(){ c.Network("net").Say("#ch", "bad-every") })
	`)

	fs := &fakeSender{}
	bus := events.NewEventBus()
	m := NewManager(bus, dir, timerTestHost(fs))
	if err := m.LoadAll(); err != nil {
		t.Fatalf("LoadAll with invalid durations should not fail: %v", err)
	}

	// Check that no invalid-duration scripts fired.
	time.Sleep(50 * time.Millisecond)
	if hasSent(fs, "bad-after") || hasSent(fs, "bad-every") {
		t.Fatal("invalid-duration timers must not fire")
	}
}

// TestSchedulerRunScriptFnUnloadedScript verifies that runScriptFn returns nil
// (not panic/error) when called for an ID that has been unloaded.
func TestSchedulerRunScriptFnUnloadedScript(t *testing.T) {
	m := NewManager(events.NewEventBus(), t.TempDir(), timerTestHost(&fakeSender{}))
	// ID never loaded — runScriptFn must be a no-op.
	if err := m.runScriptFn(extension.ID("ghost"), func() {}); err != nil {
		t.Fatalf("runScriptFn for unloaded ID returned error: %v", err)
	}
}

// TestSchedulerStopTimersIdempotent ensures stopTimers can be called multiple
// times for the same ID without panicking (reload + unload both call it).
func TestSchedulerStopTimersIdempotent(t *testing.T) {
	s := newScheduler()
	var mu sync.Mutex
	stopped := false
	s.addTimer(extension.ID("x"), func() {
		mu.Lock()
		stopped = true
		mu.Unlock()
	})
	s.stopTimers(extension.ID("x"))
	s.stopTimers(extension.ID("x")) // second call must not panic
	mu.Lock()
	if !stopped {
		t.Fatal("stop func was not called")
	}
	mu.Unlock()
}
