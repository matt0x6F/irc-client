// Copyright 2009 Thomas Jager <mail@jager.no>  All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

/*
Here's the concurrency design of this project (largely unchanged from thoj/go-ircevent):
Connect() spawns 3 goroutines (readLoop, writeLoop, pingLoop). The client then
calls Loop(), which monitors their state. Loop() will wait for them
to make a clean stop and then run another Connect(). The system can be
interrupted asynchronously by sending a message, e.g, with Privmsg(), or by
calling Reconnect() (which disconnects and forces a reconnection), or by calling
Quit(), which sends QUIT to the server and will eventually stop the Loop().

The stop mechanism is to close the (*Connection).end channel (which is only closed,
never sent-on normally), so every blocking operation in the 3 loops must also
select on `end` to make sure it stops in a timely fashion.
*/

package ircevent

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/ergochat/irc-go/ircmsg"
	"github.com/ergochat/irc-go/ircreader"
)

const (
	Version = "ergochat/irc-go"

	// prefix for keepalive ping parameters
	keepalivePrefix = "KeepAlive-"

	maxlenTags = 8192

	writeQueueSize = 10

	defaultNick = "ircevent"

	CAPTimeout = time.Second * 15
)

var (
	ClientDisconnected = errors.New("Could not send because client is disconnected")
	ServerTimedOut     = errors.New("Server did not respond in time")
	ServerDisconnected = errors.New("Disconnected by server")
	SASLFailed         = errors.New("SASL setup timed out. Does the server support SASL?")

	CapabilityNotNegotiated = errors.New("The IRCv3 capability required for this was not negotiated")
	NoLabeledResponse       = errors.New("The server failed to send a labeled response to the command")

	serverDidNotQuit = errors.New("server did not respond to QUIT")
	ClientHasQuit    = errors.New("client has called Quit()")
)

// Call this on an error forcing a disconnection:
// record the error, then close the `end` channel, stopping all goroutines
func (irc *Connection) setError(err error) {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	if irc.lastError == nil {
		irc.lastError = err
		irc.closeEndNoMutex()
	}
}

func (irc *Connection) getError() error {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	return irc.lastError
}

// LastError returns the transport/protocol error that stopped the current
// connection. It may be nil for a clean or locally initiated teardown.
func (irc *Connection) LastError() error {
	return irc.getError()
}

// Send a keepalive PING in our timestamp-based format
func (irc *Connection) ping() {
	param := fmt.Sprintf("%s%d", keepalivePrefix, time.Now().UnixNano())
	irc.Send("PING", param)
}

// LastReadNano returns the UnixNano timestamp of the most recent inbound line, or 0
// before the first read. Exposed so callers can probe link liveness on demand (e.g.
// after a system wake) by observing whether a keepalive PONG advanced it.
func (irc *Connection) LastReadNano() int64 {
	return irc.lastReadNano.Load()
}

// SendKeepalivePing sends one keepalive PING out of band of the pingLoop's schedule,
// in the same timestamped format. Used to probe whether a link that may have died
// during sleep is still answering, without waiting for the next scheduled keepalive.
// A PONG (or any inbound line) advances LastReadNano.
func (irc *Connection) SendKeepalivePing() {
	irc.ping()
}

// Interpret the PONG from a keepalive ping
func (irc *Connection) recordPong(param string) {
	ts := strings.TrimPrefix(param, keepalivePrefix)
	if ts == param {
		return
	}
	t, err := strconv.ParseInt(ts, 10, 64)
	if err != nil {
		return
	}
	if irc.Debug {
		pong := time.Unix(0, t)
		irc.Log.Printf("Lag: %v\n", time.Since(pong))
	}

	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	irc.pingSent = false
}

// Read data from a connection. To be used as a goroutine.
func (irc *Connection) readLoop() {
	defer irc.wg.Done()

	defer func() {
		if irc.registered {
			irc.runDisconnectCallbacks()
		}
	}()

	msgChan := make(chan string)
	errChan := make(chan error)
	go readMsgLoop(irc.socket, irc.MaxLineLen, msgChan, errChan, irc.end)

	lastExpireCheck := time.Now()

	for {
		select {
		case <-irc.end:
			return
		case msg := <-msgChan:
			// Stamp inbound activity for the liveness watchdog. Any line counts
			// (server PONG to our keepalive PING, or ordinary traffic); this is
			// what keeps a healthy-but-quiet link from being force-closed.
			irc.lastReadNano.Store(time.Now().UnixNano())
			if irc.Debug {
				irc.Log.Printf("<-- %s\n", strings.TrimSpace(msg))
			}

			parsedMsg, err := ircmsg.ParseLine(msg)
			if err == nil {
				irc.runCallbacks(parsedMsg)
			} else {
				irc.Log.Printf("invalid message from server: %v\n", err)
			}
		case err := <-errChan:
			irc.setError(err)
			return
		}

		if irc.batchNegotiated() && time.Since(lastExpireCheck) > irc.Timeout {
			irc.expireBatches(false)
			lastExpireCheck = time.Now()
		}
	}
}

func readMsgLoop(socket net.Conn, maxLineLen int, msgChan chan string, errChan chan error, end chan empty) {
	var reader ircreader.Reader
	reader.Initialize(socket, 1024, maxLineLen+maxlenTags)
	for {
		msgBytes, err := reader.ReadLine()
		if err == nil {
			select {
			case msgChan <- string(msgBytes):
			case <-end:
				return
			}
		} else {
			select {
			case errChan <- err:
			case <-end:
			}
			return
		}
	}
}

// Loop to write to a connection. To be used as a goroutine.
func (irc *Connection) writeLoop() {
	defer irc.wg.Done()

	for {
		select {
		case <-irc.end:
			return
		case b := <-irc.pwrite:
			if len(b) == 0 {
				continue
			}

			if irc.Debug {
				irc.Log.Printf("--> %s\n", bytes.TrimSpace(b))
			}

			if irc.Timeout != 0 {
				irc.socket.SetWriteDeadline(time.Now().Add(irc.Timeout))
			}
			_, err := irc.socket.Write(b)
			if irc.Timeout != 0 {
				irc.socket.SetWriteDeadline(time.Time{})
			}
			if err != nil {
				irc.setError(err)
				return
			}
		}
	}
}

// check the status of the connection and take appropriate action
func (irc *Connection) processTick(tick int) {
	var err error
	var shouldPing, shouldRenick bool

	defer func() {
		if err != nil {
			irc.setError(err)
			return
		}
		if shouldPing {
			irc.ping()
		}
		if shouldRenick {
			irc.Send("NICK", irc.PreferredNick())
		}
	}()

	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()

	// XXX: handle the server ignoring QUIT
	if irc.quit && time.Since(irc.quitAt) >= irc.Timeout {
		err = serverDidNotQuit
		return
	}
	if irc.pingSent {
		// unacked PING is fatal
		err = ServerTimedOut
		return
	}
	pingModulus := int(irc.KeepAlive / irc.Timeout)
	if tick%pingModulus == 0 {
		shouldPing = true
		irc.pingSent = true
		if irc.currentNick != irc.Nick {
			shouldRenick = true
		}
	}
	return
}

// handles all periodic tasks for the connection:
// 1. sending PING approximately every KeepAlive seconds, monitoring for PONG
// 2. fixing up NICK if we didn't get our preferred one
func (irc *Connection) pingLoop() {
	ticker := time.NewTicker(irc.Timeout)

	defer func() {
		irc.wg.Done()
		ticker.Stop()
	}()

	tick := 0
	for {
		select {
		case <-irc.end:
			return
		case <-ticker.C:
			tick++
			irc.processTick(tick)
		}
	}
}

// livenessWatchdog is a netpoller-independent backstop for the pingLoop.
//
// The pingLoop declares a dead link by observing its own unacked keepalive PING,
// but that detection runs THROUGH the socket. On a half-open connection — the
// network path is gone, no FIN/EOF is ever delivered, and (because the same dead
// fd backs them) the read/write deadlines never fire — writeLoop can wedge in a
// Write and the keepalive PING then blocks trying to enqueue onto a full write
// queue, wedging the pingLoop goroutine itself before it can escalate. The read
// is parked in a deadline-less ReadLine. Nothing reaches setError, `end` never
// closes, and the connection zombies "connected" forever (observed in the field:
// the fd held in TCP CLOSED for hours with the UI still showing Connected and no
// DisconnectCallback ever firing).
//
// This watchdog shares none of that machinery. It is driven by a plain ticker (a
// runtime timer, not the fd's netpoller) and, if no inbound line has arrived for
// longer than KeepAlive+2*Timeout, force-closes the socket. Close() is a syscall
// that does not depend on the netpoller, so it reliably unblocks both the parked
// read and the stuck write, letting the normal teardown -> DisconnectCallback ->
// reconnect path run. On a healthy link the server's PONG to our keepalive PING
// (and any other traffic) refreshes the timestamp well within the deadline, so
// this never fires spuriously — it only ever catches a genuinely silent socket
// that the pingLoop failed to.
func (irc *Connection) livenessWatchdog() {
	defer irc.wg.Done()

	// KeepAlive+2*Timeout leaves a full ping/pong round-trip of margin beyond the
	// pingLoop's own ~KeepAlive+Timeout detection window: on a live link we read a
	// PONG every KeepAlive, so silence this long means the socket is truly gone.
	deadline := irc.KeepAlive + 2*irc.Timeout

	ticker := time.NewTicker(irc.Timeout)
	defer ticker.Stop()

	for {
		select {
		case <-irc.end:
			return
		case <-ticker.C:
			last := irc.lastReadNano.Load()
			if last == 0 {
				continue // not yet seeded (shouldn't happen post-connect)
			}
			if time.Since(time.Unix(0, last)) > deadline {
				// Force the socket closed from outside the wedged read/write
				// loops; the resulting error drives setError -> end-close ->
				// runDisconnectCallbacks, exactly as a clean drop would.
				_ = irc.ForceClose()
				return
			}
		}
	}
}

func (irc *Connection) isQuitting() bool {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	return irc.quit
}

// Main loop to control the connection.
func (irc *Connection) Loop() {
	var lastReconnect time.Time
	for {
		irc.waitForStop()

		if irc.isQuitting() {
			return
		}

		if err := irc.getError(); err != nil {
			irc.Log.Printf("Error, disconnected: %s\n", err)
		}

		delay := time.Until(lastReconnect.Add(irc.ReconnectFreq))
		if delay > 0 {
			if irc.Debug {
				irc.Log.Printf("Waiting %v to reconnect", delay)
			}
			t := time.NewTimer(delay)
			select {
			case <-t.C:
			case <-irc.reconnSig:
				t.Stop()
			}
		}

		lastReconnect = time.Now()
		// Re-check after the reconnect delay: a Quit() that landed while we were
		// waiting (or while the previous connection was being torn down) must not
		// be honored one reconnect too late.
		if irc.isQuitting() {
			return
		}
		err := irc.Connect()
		if err != nil {
			// we are still stopped, the stop checks will return immediately
			irc.Log.Printf("Error while reconnecting: %s\n", err)
		}
	}
}

// wait for all goroutines to stop. XXX: this is not safe for concurrent
// use, call only from Connect() and Loop() (which will be on the same
// client goroutine)
func (irc *Connection) waitForStop() {
	<-irc.end
	irc.wg.Wait() // wait for readLoop and pingLoop to terminate fully

	if irc.socket != nil {
		irc.socket.Close()
	}

	irc.expireBatches(true)
}

// Quit the current connection and disconnect from the server
// RFC 1459 details: https://tools.ietf.org/html/rfc1459#section-4.1.6
func (irc *Connection) Quit() {
	irc.QuitWithMessage(irc.QuitMessage)
}

// QuitWithMessage disconnects with a per-call reason without mutating the
// connection's public configuration fields. It is safe to call concurrently
// with callbacks and teardown.
func (irc *Connection) QuitWithMessage(quitMessage string) {
	if quitMessage == "" {
		quitMessage = irc.Version
	}

	now := time.Now()
	irc.stateMutex.Lock()
	irc.quit = true
	irc.quitAt = now
	irc.stateMutex.Unlock()

	// the server will respond to this by closing our connection;
	// if it doesn't, pingLoop will eventually notice and close it
	irc.Send("QUIT", quitMessage)
}

// ForceClose closes the underlying socket directly, unblocking any goroutine
// parked in a read or write on it. Unlike Quit — which only sends a QUIT line and
// relies on the server (or the ping loop) to notice and close the link — this is
// the one teardown that reliably frees a read parked in a deadline-less
// reader.ReadLine on a dead socket. That is exactly the OS-sleep case: the TCP
// connection is gone but the netpoller never delivers EOF for the stale fd, so
// the read would otherwise block forever and the client would stay "connected"
// over a corpse. The resulting read error flows through setError -> end-close ->
// the normal disconnect path (runDisconnectCallbacks). Safe to call concurrently
// with the read/write loops and idempotent: a second close on an already-closed
// socket just returns an error, which callers may ignore.
func (irc *Connection) ForceClose() error {
	irc.stateMutex.Lock()
	socket := irc.socket
	irc.stateMutex.Unlock()
	if socket == nil {
		return nil
	}
	return socket.Close()
}

func (irc *Connection) sendInternal(b []byte) (err error) {
	// XXX ensure that (end, pwrite) are from the same instantiation of Connect;
	// invocations of this function from callbacks originating in readLoop
	// do not need this synchronization (indeed they cannot occur at a time when
	// `end` is closed), but invocations from outside do (even though the race window
	// is very small).
	irc.stateMutex.Lock()
	running := irc.running
	end := irc.end
	pwrite := irc.pwrite
	irc.stateMutex.Unlock()

	if !running {
		return ClientDisconnected
	}

	select {
	case pwrite <- b:
		return nil
	case <-end:
		return ClientDisconnected
	}
}

// Send a built ircmsg.Message.
func (irc *Connection) SendIRCMessage(msg ircmsg.Message) error {
	b, err := msg.LineBytesStrict(true, irc.MaxLineLen)
	if err != nil && !(irc.AllowTruncation && err == ircmsg.ErrorBodyTooLong) {
		if irc.Debug {
			irc.Log.Printf("couldn't assemble message: %v\n", err)
		}
		return err
	}
	return irc.sendInternal(b)
}

// Send an IRC message with tags.
func (irc *Connection) SendWithTags(tags map[string]string, command string, params ...string) error {
	return irc.SendIRCMessage(ircmsg.MakeMessage(tags, "", command, params...))
}

// Send an IRC message without tags.
func (irc *Connection) Send(command string, params ...string) error {
	return irc.SendWithTags(nil, command, params...)
}

// SendWithLabel sends an IRC message using the IRCv3 labeled-response specification.
// Instead of being processed by normal event handlers, the server response to the
// command will be collected into a *Batch and passed to the provided callback.
// If the server fails to respond correctly, the callback will be invoked with `nil`
// as the argument.
func (irc *Connection) SendWithLabel(callback func(*Batch), tags map[string]string, command string, params ...string) error {
	if !irc.labelNegotiated() {
		return CapabilityNotNegotiated
	}

	label := irc.registerLabel(callback)

	msg := ircmsg.MakeMessage(tags, "", command, params...)
	msg.SetTag("label", label)
	err := irc.SendIRCMessage(msg)
	if err != nil {
		irc.unregisterLabel(label)
	}
	return err
}

// GetLabeledResponse sends an IRC message using the IRCv3 labeled-response
// specification, then synchronously waits for the response, which is returned
// as a *Batch. If the server fails to respond correctly, an error will be
// returned.
func (irc *Connection) GetLabeledResponse(tags map[string]string, command string, params ...string) (batch *Batch, err error) {
	done := make(chan empty)
	err = irc.SendWithLabel(func(b *Batch) {
		batch = b
		close(done)
	}, tags, command, params...)
	if err != nil {
		return
	}
	<-done
	if batch == nil {
		err = NoLabeledResponse
	}
	return
}

// Send a raw string.
func (irc *Connection) SendRaw(message string) error {
	mlen := len(message)
	buf := make([]byte, mlen+2)
	copy(buf[:mlen], message[:])
	copy(buf[mlen:], "\r\n")
	return irc.sendInternal(buf)
}

// Use the connection to join a given channel.
// RFC 1459 details: https://tools.ietf.org/html/rfc1459#section-4.2.1
func (irc *Connection) Join(channel string) error {
	return irc.Send("JOIN", channel)
}

// Leave a given channel.
// RFC 1459 details: https://tools.ietf.org/html/rfc1459#section-4.2.2
func (irc *Connection) Part(channel string) error {
	return irc.Send("PART", channel)
}

// Send a notification to a nickname. This is similar to Privmsg but must not receive replies.
// RFC 1459 details: https://tools.ietf.org/html/rfc1459#section-4.4.2
func (irc *Connection) Notice(target, message string) error {
	return irc.Send("NOTICE", target, message)
}

// Send a formated notification to a nickname.
// RFC 1459 details: https://tools.ietf.org/html/rfc1459#section-4.4.2
func (irc *Connection) Noticef(target, format string, a ...interface{}) error {
	return irc.Notice(target, fmt.Sprintf(format, a...))
}

// Send (private) message to a target (channel or nickname).
// RFC 1459 details: https://tools.ietf.org/html/rfc1459#section-4.4.1
func (irc *Connection) Privmsg(target, message string) error {
	return irc.Send("PRIVMSG", target, message)
}

// Send formated string to specified target (channel or nickname).
func (irc *Connection) Privmsgf(target, format string, a ...interface{}) error {
	return irc.Privmsg(target, fmt.Sprintf(format, a...))
}

// Send (action) message to a target (channel or nickname).
// No clear RFC on this one...
func (irc *Connection) Action(target, message string) error {
	return irc.Privmsg(target, fmt.Sprintf("\001ACTION %s\001", message))
}

// Send formatted (action) message to a target (channel or nickname).
func (irc *Connection) Actionf(target, format string, a ...interface{}) error {
	return irc.Action(target, fmt.Sprintf(format, a...))
}

// Set (new) nickname.
// RFC 1459 details: https://tools.ietf.org/html/rfc1459#section-4.1.2
func (irc *Connection) SetNick(n string) {
	irc.stateMutex.Lock()
	irc.Nick = n
	irc.stateMutex.Unlock()

	irc.Send("NICK", n)
}

// Determine nick currently used with the connection.
func (irc *Connection) CurrentNick() string {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	return irc.currentNick
}

// Returns the expected or desired nickname for the connection;
// if the real nickname is different, the client will periodically
// attempt to change to this one.
func (irc *Connection) PreferredNick() string {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	return irc.Nick
}

func (irc *Connection) setCurrentNick(nick string) {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	irc.currentNick = nick
}

// Return IRCv3 CAPs actually enabled on the connection, together
// with their values if applicable. The resulting map is shared,
// so do not modify it.
func (irc *Connection) AcknowledgedCaps() (result map[string]string) {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	return irc.capsAcked
}

// Returns the 005 RPL_ISUPPORT tokens sent by the server when the
// connection was initiated, parsed into key-value form as a map.
// The resulting map is shared, so do not modify it.
func (irc *Connection) ISupport() (result map[string]string) {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	// XXX modifications to isupport are not permitted after registration
	return irc.isupport
}

// Returns true if the connection is connected to an IRC server.
func (irc *Connection) Connected() bool {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	return irc.running
}

// Reconnect forces the client to reconnect to the server.
// TODO try to ensure buffered messages are sent?
func (irc *Connection) Reconnect() {
	irc.closeEnd()
	select {
	case irc.reconnSig <- empty{}:
	default:
	}
}

func (irc *Connection) closeEnd() {
	irc.stateMutex.Lock()
	defer irc.stateMutex.Unlock()
	irc.closeEndNoMutex()
}

func (irc *Connection) closeEndNoMutex() {
	if irc.running {
		irc.running = false
		close(irc.end)
	}
}

func (irc *Connection) dial() (socket net.Conn, err error) {
	if irc.DialContext == nil {
		irc.DialContext = (&net.Dialer{}).DialContext
	}
	ctx, cancel := context.WithTimeout(context.Background(), irc.Timeout)
	defer cancel()
	socket, err = irc.DialContext(ctx, "tcp", irc.Server)
	if err != nil {
		return
	}
	if !irc.UseTLS {
		return
	}

	// see tls.DialWithDialer
	if irc.TLSConfig == nil {
		irc.TLSConfig = &tls.Config{}
	}
	if irc.TLSConfig.ServerName == "" && !irc.TLSConfig.InsecureSkipVerify {
		host, _, err := net.SplitHostPort(irc.Server)
		if err == nil {
			irc.TLSConfig.ServerName = host
		} else {
			irc.TLSConfig.ServerName = irc.Server
		}
	}
	tlsSocket := tls.Client(socket, irc.TLSConfig)
	err = tlsSocket.HandshakeContext(ctx)
	if err != nil {
		socket.Close()
		return nil, err
	}
	return tlsSocket, nil
}

// Connect to a given server using the current connection configuration.
// This function also takes care of identification if a password is provided.
// RFC 1459 details: https://tools.ietf.org/html/rfc1459#section-4.1
func (irc *Connection) Connect() (err error) {
	// invariant: after Connect we are in one of two states:
	// (a) success: return nil, socket open, goroutines launched, ready for Loop
	// (b) failure: return error, socket closed, goroutines stopped,
	//     ready for another call to Connect (possibly from Loop)
	err = func() error {
		irc.stateMutex.Lock()
		defer irc.stateMutex.Unlock()

		if irc.quit {
			return ClientHasQuit // check this again in case of Quit() while we were asleep
		}

		// mark Server as stopped since there can be an error during connect
		irc.running = false
		irc.socket = nil
		irc.currentNick = ""
		irc.lastError = nil
		irc.pingSent = false

		if irc.Server == "" {
			return errors.New("No server provided")
		}
		if len(irc.Nick) == 0 {
			irc.Nick = defaultNick
		}
		if irc.User == "" {
			irc.User = irc.Nick
		}
		if irc.RealName == "" {
			irc.RealName = irc.User
		}
		if irc.Log == nil {
			irc.Log = log.New(os.Stdout, "", log.LstdFlags)
		}
		if irc.KeepAlive == 0 {
			irc.KeepAlive = 4 * time.Minute
		}
		if irc.Timeout == 0 {
			irc.Timeout = 1 * time.Minute
		}
		if irc.KeepAlive < irc.Timeout {
			return errors.New("KeepAlive must be at least Timeout")
		}
		if irc.ReconnectFreq == 0 {
			irc.ReconnectFreq = 2 * time.Minute
		}
		if irc.SASLLogin != "" && irc.SASLPassword != "" {
			irc.UseSASL = true
		}
		if irc.UseSASL {
			// ensure 'sasl' is in the cap list if necessary
			if !sliceContains("sasl", irc.RequestCaps) {
				irc.RequestCaps = append(irc.RequestCaps, "sasl")
			}
		}
		if irc.SASLMech == "" {
			irc.SASLMech = "PLAIN"
		}
		if irc.SASLMechanism == nil && !(irc.SASLMech == "PLAIN" || irc.SASLMech == "EXTERNAL") {
			return fmt.Errorf("unsupported SASL mechanism %s", irc.SASLMech)
		}
		if irc.SASLMechanism != nil && irc.SASLMech != irc.SASLMechanism.Name() {
			return fmt.Errorf("SASLMech %q does not match SASLMechanism.Name() %q", irc.SASLMech, irc.SASLMechanism.Name())
		}
		if irc.MaxLineLen == 0 {
			irc.MaxLineLen = 512
		}
		if irc.Version == "" {
			irc.Version = Version
		}
		// this only runs on first Connect() invocation;
		// unlike other synch primitives it is shared across reconnections:
		if irc.reconnSig == nil {
			irc.reconnSig = make(chan empty)
		}
		return nil
	}()

	if err != nil {
		return err
	}

	irc.setupCallbacks()

	if irc.Debug {
		irc.Log.Printf("Connecting to %s (TLS: %t)\n", irc.Server, irc.UseTLS)
	}

	socket, err := irc.dial()
	if err != nil {
		return err
	}

	if irc.Debug {
		irc.Log.Printf("Connected to %s (%s)\n", irc.Server, socket.RemoteAddr())
	}

	// reset all connection state
	irc.stateMutex.Lock()
	irc.socket = socket
	irc.running = true
	irc.end = make(chan empty)
	irc.pwrite = make(chan []byte, writeQueueSize)
	// Seed liveness now so the watchdog measures silence from connect, not from
	// the zero time (which would make it fire immediately).
	irc.lastReadNano.Store(time.Now().UnixNano())
	irc.wg.Add(4)
	irc.capsChan = make(chan capResult, len(irc.RequestCaps))
	irc.capsLSChan = make(chan empty)
	irc.capsLSDone = false
	irc.saslChan = make(chan saslResult, 1)
	irc.saslBuffer = nil
	irc.welcomeChan = make(chan empty)
	irc.registered = false
	irc.isupportPartial = make(map[string]string)
	irc.isupport = nil
	irc.capsAcked = make(map[string]string)
	irc.capsAdvertised = nil
	irc.stateMutex.Unlock()
	irc.batchMutex.Lock()
	irc.batches = make(map[string]batchInProgress)
	irc.labelCallbacks = make(map[int64]pendingLabel)
	irc.labelCounter = 0
	irc.batchMutex.Unlock()

	go irc.readLoop()
	go irc.writeLoop()
	go irc.pingLoop()
	go irc.livenessWatchdog()

	// now we have an open socket and goroutines; we need to clean up
	// if there's a layer 7 failure
	defer func() {
		if err != nil {
			irc.closeEnd()
			irc.waitForStop()
		}
	}()

	return irc.performHandshake()
}

func (irc *Connection) performHandshake() error {
	if len(irc.WebIRC) > 0 {
		irc.Send("WEBIRC", irc.WebIRC...)
	}

	if len(irc.Password) > 0 {
		irc.Send("PASS", irc.Password)
	}

	capsRequested := len(irc.RequestCaps) != 0
	remainingCaps := 0
	acknowledgedCaps := make([]string, 0, len(irc.RequestCaps))

	if capsRequested {
		// Ask what the server supports; the actual REQ waits for the reply below.
		irc.Send("CAP", "LS", "302")
	}
	// Send NICK and USER now (not after the LS reply): a server that ignores CAP
	// never answers CAP LS and only reaches 001 once it has NICK/USER, so deferring
	// them would deadlock the LS wait until the timeout.
	irc.Send("NICK", irc.PreferredNick())
	irc.Send("USER", irc.User, "s", "e", irc.RealName)

	// Three possibilities:
	// 1. The server doesn't support CAP or we didn't request any CAPs;
	// the server will terminate registration with NICK/USER and send 001
	// 2. The server supports CAPs and will start sending CAP LS / ACK / NAK replies
	// 3. We time out before getting an intelligible response, so set a timer:
	timer := time.NewTimer(irc.Timeout)
	defer timer.Stop()

	if capsRequested {
		// Wait for the CAP LS advertisement, then request only the caps the server
		// actually offers, as ONE `CAP REQ` line. Two reasons this must be a single
		// filtered line rather than one REQ per cap:
		//   - IRCv3 CAP REQ is atomic: a combined request naming any cap the server
		//     did not advertise is rejected (NAK'd) as a whole, so we filter first.
		//   - Requesting each cap on its own line trips server anti-flood throttling
		//     (Libera released ~1 line/sec), stretching registration to ~15-20s.
	LSWAIT:
		for {
			select {
			case <-irc.capsLSChan:
				break LSWAIT // advertisement complete
			case <-irc.welcomeChan:
				capsRequested = false // server ignored CAP and went straight to 001
				break LSWAIT
			case <-timer.C:
				return ServerTimedOut
			case <-irc.end:
				return ServerDisconnected
			}
		}

		if capsRequested {
			toRequest := irc.capsToRequest()
			remainingCaps = len(toRequest)
			if remainingCaps > 0 {
				irc.Send("CAP", "REQ", strings.Join(toRequest, " "))
			}
		}
	}

CAPLOOP:
	for remainingCaps > 0 {
		select {
		case result := <-irc.capsChan:
			remainingCaps--
			if result.ack {
				acknowledgedCaps = append(acknowledgedCaps, result.capName)
			}
		case <-irc.welcomeChan:
			break CAPLOOP // server ended registration early
		case <-timer.C:
			return ServerTimedOut
		case <-irc.end:
			return ServerDisconnected
		}
	}

	irc.processAckedCaps(acknowledgedCaps)

	saslSucceeded := false
	var saslError error

	if irc.UseSASL && sliceContains("sasl", acknowledgedCaps) {
		// perform SASL and wait synchronously for the result;
		// we must wait because on conventional ircd+services stacks,
		// CAP END will terminate an in-progress SASL session
		irc.Send("AUTHENTICATE", irc.SASLMech)

		select {
		case res := <-irc.saslChan:
			saslSucceeded = !res.Failed
			if !saslSucceeded {
				saslError = res.Err
			}
		case <-timer.C:
			// technically we could proceed, but our view of the
			// registration timeout has expired
			return ServerTimedOut
		case <-irc.end:
			return ServerDisconnected
		}
	}

	if irc.UseSASL && !irc.SASLOptional && !saslSucceeded {
		if saslError == nil {
			saslError = SASLFailed
		}
		return saslError
	}

	// if we did successful CAP negotiation with the server
	// then we need CAP END to terminate registration
	if capsRequested && remainingCaps <= 0 {
		irc.Send("CAP", "END")
	}

	// wait for registration to complete, or fail
	select {
	case <-irc.welcomeChan:
		return nil
	case <-timer.C:
		return ServerTimedOut
	case <-irc.end:
		return ServerDisconnected
	}
}
