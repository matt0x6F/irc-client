package unfurl

import (
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"syscall"
	"time"
)

// ErrBlocked is returned when a URL resolves to a non-permitted address or uses
// a non-permitted scheme. Callers map it to a user-facing "blocked" status.
var ErrBlocked = errors.New("unfurl: address not permitted")

// ipGuard reports whether an address may be dialed. A nil error permits it.
type ipGuard func(addr netip.Addr) error

// cgnat is the 100.64.0.0/10 carrier-grade NAT range (RFC 6598), which net's
// IsPrivate() does not cover.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// defaultIPGuard rejects any address that is not a routable public unicast host.
// This is the anti-SSRF core: it runs against the *resolved* IP at dial time, so
// DNS rebinding and redirect-to-internal both hit it.
func defaultIPGuard(addr netip.Addr) error {
	addr = addr.Unmap() // collapse ::ffff:a.b.c.d to the IPv4 form before classifying
	switch {
	case !addr.IsValid(),
		addr.IsLoopback(),
		addr.IsPrivate(),
		addr.IsLinkLocalUnicast(),
		addr.IsLinkLocalMulticast(),
		addr.IsMulticast(),
		addr.IsUnspecified(),
		addr.IsInterfaceLocalMulticast(),
		cgnat.Contains(addr):
		return fmt.Errorf("%w: %s", ErrBlocked, addr)
	}
	return nil
}

// newGuardedClient returns an http.Client whose dialer enforces guard on the
// actual resolved IP for every connection (including each redirect hop), caps
// redirects, and never follows to a non-http(s) scheme.
func newGuardedClient(timeout time.Duration, maxRedirects int, guard ipGuard) *http.Client {
	dialer := &net.Dialer{
		Timeout: timeout,
		// Control runs after DNS resolution with the concrete IP:port about to be
		// dialed — the correct, rebinding-proof place to validate.
		Control: func(network, address string, c syscall.RawConn) error {
			return guardAddress(address, guard)
		},
	}
	return &http.Client{
		Timeout:   timeout,
		Transport: &http.Transport{DialContext: dialer.DialContext},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= maxRedirects {
				return fmt.Errorf("unfurl: stopped after %d redirects", maxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("%w: scheme %q", ErrBlocked, req.URL.Scheme)
			}
			return nil
		},
	}
}

func guardAddress(address string, guard ipGuard) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrBlocked, err)
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("%w: unresolved host %q", ErrBlocked, host)
	}
	return guard(addr)
}
