package unfurl

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"net/netip"
	"testing"
	"time"
)

func TestDefaultIPGuard(t *testing.T) {
	cases := []struct {
		addr    string
		blocked bool
	}{
		{"1.1.1.1", false},              // public
		{"8.8.8.8", false},              // public
		{"127.0.0.1", true},             // loopback
		{"10.0.0.5", true},              // private
		{"172.16.4.2", true},            // private
		{"192.168.1.1", true},           // private
		{"169.254.169.254", true},       // link-local (cloud metadata)
		{"100.64.0.1", true},            // CGNAT
		{"0.0.0.0", true},               // unspecified
		{"224.0.0.1", true},             // multicast
		{"::1", true},                   // IPv6 loopback
		{"fc00::1", true},               // IPv6 unique-local (private)
		{"fe80::1", true},               // IPv6 link-local
		{"::ffff:10.0.0.1", true},       // IPv4-mapped private
		{"2606:4700:4700::1111", false}, // public IPv6
	}
	for _, c := range cases {
		addr := netip.MustParseAddr(c.addr)
		err := defaultIPGuard(addr)
		if (err != nil) != c.blocked {
			t.Errorf("defaultIPGuard(%s): got err=%v, want blocked=%v", c.addr, err, c.blocked)
		}
	}
}

func TestGuardedClientRejectsLoopback(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer srv.Close()

	client := newGuardedClient(5*time.Second, 3, defaultIPGuard)
	_, err := client.Get(srv.URL) // httptest binds 127.0.0.1 → must be blocked
	if !errors.Is(err, ErrBlocked) {
		t.Fatalf("expected ErrBlocked dialing loopback, got %v", err)
	}
}
