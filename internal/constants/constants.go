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
)

