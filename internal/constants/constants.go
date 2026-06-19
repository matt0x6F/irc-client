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

	// ConnectionReadTimeout is how long a socket read/write may stall before the
	// library treats the connection as dead (enforced via socket deadlines).
	ConnectionReadTimeout = 30 * time.Second
	// ConnectionKeepAlive is how often the library sends a keepalive PING. The
	// library requires KeepAlive >= Timeout.
	ConnectionKeepAlive = 60 * time.Second
	// ConnectionStaleThreshold is how long the client may go with NO inbound
	// traffic at all before the watchdog forces a teardown. Must be > KeepAlive
	// so a healthy idle link (which still gets PING/PONG every KeepAlive) is
	// never flagged.
	ConnectionStaleThreshold = 90 * time.Second
	// ConnectionWatchdogInterval is how often the watchdog re-checks staleness.
	ConnectionWatchdogInterval = 15 * time.Second
)
