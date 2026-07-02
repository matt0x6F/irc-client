package irc

import (
	"bufio"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// mockServer accepts one client and records the exact order of lines it sends.
type mockServer struct {
	ln    net.Listener
	mu    sync.Mutex
	lines []string
	// script: respond to CAP LS/REQ + AUTHENTICATE, then 903 + 001
	failSASL bool
}

func newMockServer(t *testing.T, failSASL bool) *mockServer {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &mockServer{ln: ln, failSASL: failSASL}
	go s.serve()
	return s
}

func (s *mockServer) addr() string { return s.ln.Addr().String() }

func (s *mockServer) record(line string) {
	s.mu.Lock()
	s.lines = append(s.lines, line)
	s.mu.Unlock()
}

func (s *mockServer) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]string(nil), s.lines...)
}

func (s *mockServer) serve() {
	conn, err := s.ln.Accept()
	if err != nil {
		return
	}
	defer conn.Close()
	r := bufio.NewReader(conn)
	w := func(line string) { conn.Write([]byte(line + "\r\n")) }
	capLSSeen := false
	for {
		line, err := r.ReadString('\n')
		if err != nil {
			return
		}
		line = strings.TrimRight(line, "\r\n")
		s.record(line)
		up := strings.ToUpper(line)
		switch {
		case strings.HasPrefix(up, "CAP LS"):
			capLSSeen = true
			w(":mock CAP * LS :sasl server-time message-tags")
		case strings.HasPrefix(up, "CAP REQ"):
			caps := line[strings.Index(line, ":")+1:]
			w(":mock CAP * ACK :" + caps)
		case strings.HasPrefix(up, "USER"):
			// A real ircd suspends registration until CAP END only if the client
			// began CAP negotiation. With no CAP LS (the old, broken path) it
			// registers immediately on NICK/USER — which is exactly the bug.
			//
			// The library's Connect() does not treat 001 (RPL_WELCOME) itself as
			// "registration complete" — it waits on RPL_ENDOFMOTD (376) or
			// ERR_NOMOTD (422) to unblock (see handleRegistration in the fork).
			// So the mock must send end-of-MOTD too, or Connect() blocks for the
			// full irc.Timeout instead of failing the intended assertion.
			if !capLSSeen {
				w(":mock 001 nick :Welcome")
				w(":mock 376 nick :End of /MOTD command")
			}
		case strings.HasPrefix(up, "AUTHENTICATE PLAIN"):
			w("AUTHENTICATE +")
		case strings.HasPrefix(up, "AUTHENTICATE") && !strings.Contains(up, "PLAIN"):
			if s.failSASL {
				w(":mock 904 nick :SASL authentication failed")
			} else {
				w(":mock 900 nick nick!u@h account :You are now logged in")
				w(":mock 903 nick :SASL authentication successful")
			}
		case strings.HasPrefix(up, "CAP END"):
			if !s.failSASL {
				w(":mock 001 nick :Welcome")
				w(":mock 376 nick :End of /MOTD command")
			}
		}
	}
}

func testClient(t *testing.T, addr string) *IRCClient {
	host, port, _ := net.SplitHostPort(addr)
	p := 0
	for _, c := range port {
		p = p*10 + int(c-'0')
	}
	pw := "s3cret"
	mech := "PLAIN"
	user := "nick"
	n := &storage.Network{
		Name: "mock", Nickname: "nick", Username: "nick", Realname: "nick",
		Address: host, Port: p, TLS: false,
		SASLEnabled: true, SASLMechanism: &mech, SASLUsername: &user, SASLPassword: &pw,
	}
	st, err := storage.NewStorage(filepath.Join(t.TempDir(), "cascade-test.db"), 100, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	return NewIRCClient(n, events.NewEventBus(), st)
}

func TestSASLCompletesBeforeRegistration(t *testing.T) {
	srv := newMockServer(t, false)
	c := testClient(t, srv.addr())
	done := make(chan error, 1)
	go func() { done <- c.Connect() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("connect failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("connect timed out")
	}
	c.Disconnect()

	lines := srv.snapshot()
	capLS, nick, auth := -1, -1, -1
	for i, l := range lines {
		u := strings.ToUpper(l)
		if capLS == -1 && strings.HasPrefix(u, "CAP LS") {
			capLS = i
		}
		if nick == -1 && strings.HasPrefix(u, "NICK") {
			nick = i
		}
		if auth == -1 && strings.HasPrefix(u, "AUTHENTICATE") {
			auth = i
		}
	}
	// The bug: the old path sends NICK/USER with no prior CAP LS, so CAP LS is
	// absent (or arrives only post-001). The fix sends CAP LS before NICK and
	// runs AUTHENTICATE during the handshake.
	if capLS == -1 || nick == -1 {
		t.Fatalf("client never began CAP negotiation before registration: %v", lines)
	}
	if !(capLS < nick) {
		t.Fatalf("CAP LS (%d) must precede NICK (%d): %v", capLS, nick, lines)
	}
	if auth == -1 {
		t.Fatalf("no AUTHENTICATE seen — SASL did not run during the handshake: %v", lines)
	}
}

func TestSASLFailureAbortsWithAuthFailed(t *testing.T) {
	srv := newMockServer(t, true)
	c := testClient(t, srv.addr())
	err := c.Connect()
	if err == nil {
		t.Fatal("expected Connect() to fail on SASL failure")
	}
	if !c.AuthFailed() {
		t.Fatal("AuthFailed() must be true after a SASL failure")
	}
}
