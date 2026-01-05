package validation

import (
	"fmt"
	"strings"
)

// ValidateNetworkConfig validates network configuration
func ValidateNetworkConfig(name, nickname, username, realname string, servers []struct {
	Address string
	Port    int
}) error {
	if strings.TrimSpace(name) == "" {
		return fmt.Errorf("network name is required")
	}
	if strings.TrimSpace(nickname) == "" {
		return fmt.Errorf("nickname is required")
	}
	if strings.TrimSpace(username) == "" {
		return fmt.Errorf("username is required")
	}
	if strings.TrimSpace(realname) == "" {
		return fmt.Errorf("realname is required")
	}
	if len(servers) == 0 {
		return fmt.Errorf("at least one server is required")
	}
	for i, srv := range servers {
		if err := ValidateServerAddress(srv.Address, srv.Port); err != nil {
			return fmt.Errorf("server %d: %w", i+1, err)
		}
	}
	return nil
}

// ValidateChannelName validates an IRC channel name
func ValidateChannelName(channel string) error {
	channel = strings.TrimSpace(channel)
	if channel == "" {
		return fmt.Errorf("channel name is required")
	}
	// IRC channels must start with #, &, +, or !
	if len(channel) == 0 || (channel[0] != '#' && channel[0] != '&' && channel[0] != '+' && channel[0] != '!') {
		return fmt.Errorf("channel name must start with #, &, +, or !")
	}
	// Channel names have length limits (typically 50 chars, but varies by server)
	if len(channel) > 200 {
		return fmt.Errorf("channel name too long (max 200 characters)")
	}
	// Check for invalid characters
	if strings.ContainsAny(channel, " \x00\x07\x0A\x0D,") {
		return fmt.Errorf("channel name contains invalid characters")
	}
	return nil
}

// ValidateServerAddress validates a server address and port
func ValidateServerAddress(address string, port int) error {
	address = strings.TrimSpace(address)
	if address == "" {
		return fmt.Errorf("server address is required")
	}
	if port <= 0 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	return nil
}

