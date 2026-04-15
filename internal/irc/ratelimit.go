package irc

import (
	"sync"
	"time"
)

// RateLimiter implements a token bucket rate limiter for outgoing IRC messages.
// IRC servers typically allow ~4 messages per 2 seconds before throttling or
// disconnecting the client.
type RateLimiter struct {
	mu       sync.Mutex
	tokens   int
	maxBurst int           // Maximum tokens (burst capacity)
	interval time.Duration // How often a token is added
	lastAdd  time.Time
}

// NewRateLimiter creates a rate limiter that allows maxBurst messages per window.
// For example, NewRateLimiter(4, 2*time.Second) allows 4 messages per 2 seconds.
func NewRateLimiter(maxBurst int, window time.Duration) *RateLimiter {
	return &RateLimiter{
		tokens:   maxBurst,
		maxBurst: maxBurst,
		interval: window / time.Duration(maxBurst),
		lastAdd:  time.Now(),
	}
}

// Wait blocks until a token is available, then consumes it.
func (r *RateLimiter) Wait() {
	for {
		r.mu.Lock()
		r.refill()

		if r.tokens > 0 {
			r.tokens--
			r.mu.Unlock()
			return
		}

		// Calculate how long until the next token arrives
		elapsed := time.Since(r.lastAdd)
		wait := r.interval - elapsed
		if wait < 0 {
			wait = 0
		}
		r.mu.Unlock()

		// Sleep outside the lock
		if wait > 0 {
			time.Sleep(wait)
		} else {
			// Yield briefly to avoid busy-spinning
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// refill adds tokens based on elapsed time. Must be called with mu held.
func (r *RateLimiter) refill() {
	now := time.Now()
	elapsed := now.Sub(r.lastAdd)

	if elapsed >= r.interval {
		newTokens := int(elapsed / r.interval)
		r.tokens += newTokens
		if r.tokens > r.maxBurst {
			r.tokens = r.maxBurst
		}
		r.lastAdd = now
	}
}
