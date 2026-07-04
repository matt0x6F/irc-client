package ircevent

import (
	"bufio"
	"net"
	"strings"
	"testing"
	"time"
)

// TestHandshakeSendsSingleFilteredCapReq drives the real performHandshake against a
// minimal in-process IRC server over loopback and asserts the client sends exactly
// ONE `CAP REQ` line containing only the caps the server advertised — not one line
// per cap (which trips server anti-flood throttling) and not any un-advertised cap
// (which an atomic CAP REQ would get NAK'd as a whole). Deterministic; no external
// network.
func TestHandshakeSendsSingleFilteredCapReq(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	capReqLines := make(chan string, 8)
	serverDone := make(chan struct{})

	go func() {
		defer close(serverDone)
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }

		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "CAP LS"):
				// Advertise a subset: server-time & message-tags are wanted; batch is
				// wanted-and-advertised too; the client also wants a nonexistent cap
				// that we deliberately do NOT advertise.
				write(":mock CAP * LS :server-time message-tags batch")
			case strings.HasPrefix(line, "CAP REQ"):
				capReqLines <- line
				// Echo the requested caps back as an ACK.
				idx := strings.Index(line, ":")
				caps := ""
				if idx >= 0 {
					caps = line[idx+1:]
				}
				write(":mock CAP * ACK :" + caps)
			case strings.HasPrefix(line, "CAP END"):
				write(":mock 001 tester :Welcome")
				// Registration completes on end-of-MOTD (376), not 001.
				write(":mock 376 tester :End of /MOTD command.")
			case strings.HasPrefix(line, "QUIT"):
				return
			}
		}
	}()

	irc := &Connection{
		Server:      ln.Addr().String(),
		Nick:        "tester",
		User:        "tester",
		RealName:    "Tester",
		RequestCaps: []string{"server-time", "message-tags", "example.org/nonexistent"},
		Timeout:     5 * time.Second,
		KeepAlive:   5 * time.Second,
	}
	debugTest(irc)

	if err := irc.Connect(); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer irc.Quit()

	// Exactly one CAP REQ line, carrying only the advertised∩wanted caps in order.
	select {
	case got := <-capReqLines:
		want := "CAP REQ :server-time message-tags"
		if got != want {
			t.Fatalf("CAP REQ = %q, want %q", got, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("no CAP REQ line received")
	}

	select {
	case extra := <-capReqLines:
		t.Fatalf("expected a single combined CAP REQ, got a second line: %q", extra)
	default:
	}
}

// TestHandshakeNoCapReqWhenNothingAdvertised covers the path where the server
// advertises none of the caps we want: we must still terminate negotiation with
// CAP END (so registration completes) and send no CAP REQ at all.
func TestHandshakeNoCapReqWhenNothingAdvertised(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	sawCapReq := make(chan struct{}, 1)
	sawCapEnd := make(chan struct{}, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		defer conn.Close()
		r := bufio.NewReader(conn)
		write := func(s string) { _, _ = conn.Write([]byte(s + "\r\n")) }
		for {
			line, err := r.ReadString('\n')
			if err != nil {
				return
			}
			line = strings.TrimRight(line, "\r\n")
			switch {
			case strings.HasPrefix(line, "CAP LS"):
				write(":mock CAP * LS :multi-prefix chghost") // nothing the client wants
			case strings.HasPrefix(line, "CAP REQ"):
				sawCapReq <- struct{}{}
			case strings.HasPrefix(line, "CAP END"):
				sawCapEnd <- struct{}{}
				write(":mock 001 tester :Welcome")
				// Registration completes on end-of-MOTD (376), not 001.
				write(":mock 376 tester :End of /MOTD command.")
			case strings.HasPrefix(line, "QUIT"):
				return
			}
		}
	}()

	irc := &Connection{
		Server:      ln.Addr().String(),
		Nick:        "tester",
		User:        "tester",
		RealName:    "Tester",
		RequestCaps: []string{"server-time", "sasl"},
		Timeout:     5 * time.Second,
		KeepAlive:   5 * time.Second,
	}
	debugTest(irc)

	if err := irc.Connect(); err != nil {
		t.Fatalf("Connect() failed: %v", err)
	}
	defer irc.Quit()

	select {
	case <-sawCapEnd:
	case <-time.After(2 * time.Second):
		t.Fatal("CAP END not sent; registration would never complete")
	}
	select {
	case <-sawCapReq:
		t.Fatal("sent a CAP REQ even though no wanted cap was advertised")
	default:
	}
}
