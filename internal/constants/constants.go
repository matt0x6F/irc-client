package constants

import "time"

// Connection timing constants
const (
	// AutoConnectDelay is the initial delay before starting auto-connect process
	AutoConnectDelay = 1 * time.Second

	// ConnectionStaggerDelay is the delay between each network connection attempt
	ConnectionStaggerDelay = 500 * time.Millisecond

	// AutoJoinDelay is the delay after connection before auto-joining channels
	AutoJoinDelay = 2 * time.Second

	// ConnectionCleanupDelay is the delay to wait for connection cleanup
	ConnectionCleanupDelay = 500 * time.Millisecond

	// ConnectionTeardownTimeout bounds how long a deliberate Disconnect waits for
	// the library's Loop goroutine to fully exit after QUIT. A well-behaved server
	// drops the socket immediately, so this resolves in well under a second; the
	// cap only guards against a half-open socket (the read loop then ends on its
	// own read deadline). Setting the quit flag already prevents any reconnect, so
	// timing out here is harmless — it only forgoes the clean-exit wait.
	ConnectionTeardownTimeout = 5 * time.Second

	// ConnectionReadTimeout bounds how long an unacknowledged keepalive PING may
	// go before the library declares the connection dead (it is the library's
	// Timeout, enforced against the PING/PONG loop). The library requires
	// KeepAlive >= Timeout.
	ConnectionReadTimeout = 30 * time.Second
	// ConnectionKeepAlive is the idle period before the library sends a keepalive
	// PING. Detection of a dead socket (including after sleep/wake) is owned
	// entirely by this library PING/PONG loop, bounded by KeepAlive + Timeout.
	ConnectionKeepAlive = 60 * time.Second

	// WakeProbeTimeout bounds how long the wake handler waits for a keepalive PONG
	// before deciding a connection died during sleep. A live link (one macOS kept
	// alive via TCP keepalive) answers in well under a second, so this only delays
	// reconnecting a genuinely dead socket — far better than the old behavior of
	// force-reconnecting every link on every maintenance DarkWake, which rejoined
	// channels and spammed visible JOINs.
	WakeProbeTimeout = 5 * time.Second
)
