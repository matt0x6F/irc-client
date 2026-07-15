package irc

import (
	"runtime/debug"
	"sync"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/logger"
)

// applicationCallback is work handed off from irc-go's socket read goroutine.
// irc-go deliberately runs callbacks inline and will not read another server
// line until they return. Cascade callbacks perform database writes, publish
// events, and can indirectly wake plugins and the WebView, so they must run on
// a separate ordered worker instead.
type applicationCallback struct {
	name string
	run  func()
}

// callbackDispatcher is an elastic single-consumer queue. Enqueue only holds a
// mutex long enough to append, so a finite server burst can always be drained
// from the socket even when application processing is temporarily slower. One
// worker preserves the ordering assumptions the handlers historically got from
// irc-go's serial callback execution.
type callbackDispatcher struct {
	mu       sync.Mutex
	ready    *sync.Cond
	queue    []applicationCallback
	stopping bool
	done     chan struct{}
}

func newCallbackDispatcher() *callbackDispatcher {
	d := &callbackDispatcher{done: make(chan struct{})}
	d.ready = sync.NewCond(&d.mu)
	go d.run()
	return d
}

// enqueue adds work without waiting for any earlier callback to finish.
// It returns false after shutdown has begun.
func (d *callbackDispatcher) enqueue(name string, run func()) bool {
	if d == nil || run == nil {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.stopping {
		return false
	}
	d.queue = append(d.queue, applicationCallback{name: name, run: run})
	d.ready.Signal()
	return true
}

// stopAfterDrain rejects new work and lets the worker finish everything already
// received from the server. This preserves wire order through the final line and
// then releases the per-connection goroutine.
func (d *callbackDispatcher) stopAfterDrain() {
	if d == nil {
		return
	}
	d.mu.Lock()
	d.stopping = true
	d.ready.Broadcast()
	d.mu.Unlock()
}

func (d *callbackDispatcher) run() {
	defer close(d.done)
	for {
		d.mu.Lock()
		for len(d.queue) == 0 && !d.stopping {
			d.ready.Wait()
		}
		if len(d.queue) == 0 && d.stopping {
			d.mu.Unlock()
			return
		}
		work := d.queue[0]
		d.queue[0] = applicationCallback{}
		d.queue = d.queue[1:]
		// Do not retain the peak backing array forever after a registration
		// burst containing thousands of WHOX/NAMES lines.
		if len(d.queue) == 0 {
			d.queue = nil
		}
		queued := len(d.queue)
		d.mu.Unlock()

		started := time.Now()
		func() {
			defer func() {
				if recovered := recover(); recovered != nil {
					logger.Log.Error().
						Interface("panic", recovered).
						Str("callback", work.name).
						Bytes("stack", debug.Stack()).
						Msg("Panic in IRC application callback")
				}
			}()
			work.run()
		}()
		if elapsed := time.Since(started); elapsed >= 100*time.Millisecond {
			logger.Log.Warn().
				Str("callback", work.name).
				Dur("elapsed", elapsed).
				Int("queued", queued).
				Msg("Slow IRC application callback")
		}
	}
}

// addCallback registers a Cascade application callback with irc-go. The
// wrapper returns to irc-go immediately after enqueueing, allowing its read loop
// to continue draining the socket while the ordered worker performs storage,
// event, plugin, and frontend work.
func (c *IRCClient) addCallback(command string, callback func(ircmsg.Message)) {
	c.conn.AddCallback(command, func(message ircmsg.Message) {
		if c.callbacks == nil || !c.callbacks.enqueue(command, func() { callback(message) }) {
			logger.Log.Debug().Str("callback", command).Msg("Dropped IRC application callback after dispatcher shutdown")
		}
	})
}

func (c *IRCClient) addConnectCallback(callback func(ircmsg.Message)) {
	c.conn.AddConnectCallback(func(message ircmsg.Message) {
		if c.callbacks == nil || !c.callbacks.enqueue("connected", func() { callback(message) }) {
			logger.Log.Debug().Msg("Dropped IRC connect callback after dispatcher shutdown")
		}
	})
}
