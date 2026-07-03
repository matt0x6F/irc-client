package irc

import (
	"context"
	"errors"
	"log"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
)

// halfOpenConn models the live failure mode that a net.Pipe stub cannot: a TCP
// socket whose network path vanished (WiFi blip / route change / VPN) WITHOUT a
// clean FIN. The macOS netpoller stops servicing the orphaned fd, so:
//
//   - Read parks forever (no data, no EOF) until the fd is Close()d locally.
//   - Write, once the path is dead, blocks forever — and crucially the write
//     DEADLINE never fires either (the same dead netpoller backs both).
//   - SetDeadline / SetReadDeadline / SetWriteDeadline are no-ops: a dead fd's
//     deadline timers do not rescue a parked read or a stuck write.
//   - Close() is the ONE operation that still works (a syscall, not netpoller-
//     driven) and unblocks both the parked read and the stuck write.
//
// Registration writes must succeed (the client has to send NICK/USER and read
// 001/376), so writes only go dead AFTER the reader has fed the registration
// burst — mirroring the live death, which struck mid post-registration JOIN
// burst with the write queue already full.
type halfOpenConn struct {
	closeOnce sync.Once
	closed    chan struct{}

	readMu   sync.Mutex
	readData [][]byte // server lines fed to the client, in order
	readIdx  int

	writeDead atomic.Bool // set true after registration: writes then block forever
}

func newHalfOpenConn(lines [][]byte) *halfOpenConn {
	return &halfOpenConn{closed: make(chan struct{}), readData: lines}
}

func (c *halfOpenConn) Read(b []byte) (int, error) {
	c.readMu.Lock()
	if c.readIdx < len(c.readData) {
		n := copy(b, c.readData[c.readIdx])
		c.readIdx++
		last := c.readIdx == len(c.readData)
		c.readMu.Unlock()
		if last {
			// Registration burst delivered; the path now dies. Subsequent writes
			// wedge, exactly as they did live mid-burst.
			c.writeDead.Store(true)
		}
		return n, nil
	}
	c.readMu.Unlock()
	<-c.closed // park forever, like a read on a corpse — freed only by Close()
	return 0, errors.New("use of closed connection")
}

func (c *halfOpenConn) Write(b []byte) (int, error) {
	if c.writeDead.Load() {
		<-c.closed // block forever, ignoring any write deadline
		return 0, errors.New("use of closed connection")
	}
	return len(b), nil // registration writes succeed and are discarded
}

func (c *halfOpenConn) Close() error {
	c.closeOnce.Do(func() { close(c.closed) })
	return nil
}

func (c *halfOpenConn) LocalAddr() net.Addr                { return dummyAddr{} }
func (c *halfOpenConn) RemoteAddr() net.Addr               { return dummyAddr{} }
func (c *halfOpenConn) SetDeadline(t time.Time) error      { return nil }
func (c *halfOpenConn) SetReadDeadline(t time.Time) error  { return nil }
func (c *halfOpenConn) SetWriteDeadline(t time.Time) error { return nil }

type dummyAddr struct{}

func (dummyAddr) Network() string { return "tcp" }
func (dummyAddr) String() string  { return "irc.test.local:6667" }

// TestHalfOpenSocketWedgesLibraryDetection reproduces the 2026-07-03 live zombie:
// on an AWAKE machine, both IRC sockets sat in TCP CLOSED with the fd held for
// 2.5 hours, the UI stuck "Connected", and — the decisive tell — the library's
// DisconnectCallback NEVER fired (no "Disconnected from server" was ever written
// to the DB for that death).
//
// The mechanism: the path died mid post-registration burst with the library's
// write queue (pwrite, cap 10) full. writeLoop is stuck in a Write on the dead
// fd whose deadline never fires; the pingLoop's keepalive PING then blocks trying
// to enqueue onto the full queue, wedging the very goroutine that is supposed to
// declare the timeout. The read is parked in a deadline-less ReadLine. No goroutine
// can reach setError, so `end` never closes and the DisconnectCallback never runs.
//
// This test asserts the DisconnectCallback fires within a bounded time on such a
// socket. It FAILS before the fix (the callback never fires — a permanent zombie)
// and PASSES after the netpoller-independent liveness watchdog force-closes the fd.
func TestHalfOpenSocketWedgesLibraryDetection(t *testing.T) {
	lines := [][]byte{
		[]byte(":irc.test.local 001 robodan :Welcome\r\n"),
		[]byte(":irc.test.local 376 robodan :End of MOTD\r\n"),
	}
	fc := newHalfOpenConn(lines)
	t.Cleanup(func() { _ = fc.Close() })

	conn := &ircevent.Connection{
		Nick:       "robodan",
		Server:     "irc.test.local:6667",
		KeepAlive:  300 * time.Millisecond,
		Timeout:    150 * time.Millisecond,
		MaxLineLen: 512,
		Log:        log.New(discard{}, "", 0),
		DialContext: func(ctx context.Context, network, addr string) (net.Conn, error) {
			return fc, nil
		},
	}

	disconnected := make(chan struct{}, 1)
	conn.AddDisconnectCallback(func(ircmsg.Message) {
		select {
		case disconnected <- struct{}{}:
		default:
		}
	})

	if err := conn.Connect(); err != nil {
		t.Fatalf("Connect: %v", err)
	}

	c := &IRCClient{conn: conn, connected: true}
	c.runLoop()

	// Registration is done and the write path is now dead. Fill the write queue
	// the way the live JOIN/WHO/MONITOR burst did, so the pingLoop's keepalive
	// PING has nowhere to go and wedges. Done from a goroutine because these
	// sends block once the queue is full — which is precisely the trap.
	go func() {
		for range 20 {
			_ = conn.Send("PRIVMSG", "#chan", "backlog burst")
		}
	}()

	// A healthy pingLoop would declare the dead link within KeepAlive+2*Timeout
	// (~600ms). Give it 3s of headroom. If the DisconnectCallback never fires,
	// the connection is the live zombie: dead socket, UI "Connected", forever.
	select {
	case <-disconnected:
		// Liveness enforcement force-closed the wedged socket and teardown ran.
	case <-time.After(3 * time.Second):
		t.Fatal(`library never fired DisconnectCallback on a half-open socket with a wedged write path — this is the live 2.5h zombie: dead socket, UI "Connected", no teardown, no reconnect`)
	}
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }
