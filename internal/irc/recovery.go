package irc

// recovery.go holds the connection's authentication-failure state and the
// channel-join error feedback. It builds only on protocol-detectable signals
// (SASL numerics, standard join-error numerics) — it issues no services
// commands and parses no NOTICE text.

// AuthFailed reports whether SASL was enabled but did not succeed on this
// session. The app layer reads it to decide whether an auto-reconnect should be
// suppressed (a credential failure never recovers by retrying).
func (c *IRCClient) AuthFailed() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.authFailed
}

func (c *IRCClient) setAuthFailed(v bool) {
	c.mu.Lock()
	c.authFailed = v
	c.mu.Unlock()
}
