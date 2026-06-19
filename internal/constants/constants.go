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

	// ConnectionReadTimeout bounds how long an unacknowledged keepalive PING may
	// go before the library declares the connection dead (it is the library's
	// Timeout, enforced against the PING/PONG loop). The library requires
	// KeepAlive >= Timeout.
	ConnectionReadTimeout = 30 * time.Second
	// ConnectionKeepAlive is the idle period before the library sends a keepalive
	// PING. Detection of a dead socket (including after sleep/wake) is owned
	// entirely by this library PING/PONG loop, bounded by KeepAlive + Timeout.
	ConnectionKeepAlive = 60 * time.Second
)
