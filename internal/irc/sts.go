package irc

import (
	"net"
	"strconv"
	"strings"
)

// STSPolicy is the parsed value of an IRCv3 STS (Strict Transport Security)
// capability advertisement. The raw value is a comma-separated key[=value] list,
// e.g. "duration=2592000,port=6697" or "duration=0". See
// https://ircv3.net/specs/extensions/sts
type STSPolicy struct {
	// Port is the TLS port the server wants future connections to use. Meaningful
	// when advertised over a plaintext connection (the upgrade target) and persisted
	// over TLS (the bootstrap port for later plaintext connections). 0 if absent.
	Port int
	// Duration is how many seconds a TLS-advertised policy should persist. 0 means
	// "remove any stored policy" (and is also the value used over plaintext, where
	// duration is ignored). Negative/garbage values are clamped to 0.
	Duration int64
	// Present is true if the sts capability was advertised at all.
	Present bool
}

// parseSTS parses the value portion of an `sts` capability (the part after "="
// already extracted by capValue). It tolerates missing or malformed tokens:
// an unpar=>seable port or duration is treated as absent (0), and unknown tokens
// (such as the browser-oriented "preload" flag) are ignored. Present is set true
// because the caller only invokes parseSTS when the capability itself was offered.
func parseSTS(value string) STSPolicy {
	p := STSPolicy{Present: true}
	for _, token := range strings.Split(value, ",") {
		token = strings.TrimSpace(token)
		if token == "" {
			continue
		}
		key, val := token, ""
		if idx := strings.Index(token, "="); idx != -1 {
			key = token[:idx]
			val = token[idx+1:]
		}
		key = strings.TrimSpace(strings.ToLower(key))
		switch key {
		case "port":
			if n, err := strconv.Atoi(strings.TrimSpace(val)); err == nil && n > 0 {
				p.Port = n
			}
		case "duration":
			if n, err := strconv.ParseInt(strings.TrimSpace(val), 10, 64); err == nil && n > 0 {
				p.Duration = n
			}
		}
	}
	return p
}

// IsIPLiteral reports whether host is an IP address literal rather than a
// hostname. The STS spec forbids applying a policy to a connection made to an IP
// literal (you can't meaningfully pin TLS to an address the user typed directly),
// so STS is ignored for these hosts. Exported so the App's pre-connection
// enforcement shares one definition with the protocol layer.
func IsIPLiteral(host string) bool {
	return net.ParseIP(strings.TrimSpace(host)) != nil
}
