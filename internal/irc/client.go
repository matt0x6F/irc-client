package irc

import (
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/ergochat/irc-go/ircevent"
	"github.com/ergochat/irc-go/ircmsg"
	"github.com/matt0x6f/irc-client/internal/constants"
	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
	"github.com/matt0x6f/irc-client/internal/validation"
)

// requestedCaps lists the IRCv3 capabilities this client wants. We list both the
// ratified "chathistory" and the older "draft/chathistory" names; the contains()
// filter only requests whichever the server actually advertises. "batch" is
// required for CHATHISTORY (replays arrive wrapped in a BATCH that the underlying
// ergochat/irc-go library collects for us). "multi-prefix" makes the server send
// every membership prefix a user holds (e.g. "@+") in NAMES/WHO, not just the
// highest; the 353 parser already accumulates all of them into the stored modes.
// "cap-notify" lets the server announce runtime capability changes via CAP NEW /
// CAP DEL; it is implicitly enabled by CAP LS 302, but we request it explicitly
// so it surfaces in enabledCaps. The away-notify/account-notify/extended-join/
// chghost/account-tag cluster keeps the live roster current (see UserMeta).
// "extended-monitor" extends that cluster to MONITORed nicks: with it the server
// also pushes AWAY/ACCOUNT/CHGHOST/SETNAME for monitored users we share no channel
// with, so the Buddies pane / DM dots can reflect their away state. "no-implicit-names"
// suppresses the automatic NAMES reply after our JOIN; when it is ACKed we send an
// explicit NAMES so the roster still builds (see the JOIN handler).
var requestedCaps = []string{"sasl", "server-time", "echo-message", "message-tags", "batch", "draft/chathistory", "chathistory", "multi-prefix", "cap-notify", "away-notify", "account-notify", "extended-join", "chghost", "account-tag", "userhost-in-names", "setname", "invite-notify", "standard-replies", "labeled-response", "extended-monitor", "no-implicit-names"}

// IRCClient manages IRC connections
type IRCClient struct {
	conn                  *ircevent.Connection
	eventBus              *events.EventBus
	storage               *storage.Storage
	networkID             int64
	network               *storage.Network
	mu                    sync.RWMutex
	connected             bool
	abandoned             bool          // Sticky: this IRCClient is retired and must never re-register. Set on any drop (onDisconnect) and on explicit teardown (signalQuit); a later connect from the library Loop's own reconnect is then ignored by onConnect. The app recovers every drop with a fresh IRCClient, so an abandoned one is gone for good (guarded by mu)
	loopDone              chan struct{} // Closed when the library's Loop() goroutine fully exits; lets teardown wait for a clean stop (guarded by mu)
	saslEnabled           bool
	saslMechanism         string
	saslUsername          string
	saslPassword          string
	saslInProgress        bool
	saslAuthenticated     bool
	authFailed            bool // True when SASL was enabled but did not succeed this session (guarded by mu)
	saslCapRequested      bool
	saslCapAcknowledged   bool
	scramState            *SCRAMState
	namesInProgress       map[string]bool        // Track channels currently receiving NAMES list
	namesMu               sync.Mutex             // Mutex for namesInProgress map
	serverCapabilities    *ServerCapabilities    // Server capabilities from ISUPPORT
	chanTypesAtomic       atomic.Pointer[string] // CHANTYPES from ISUPPORT (e.g. "#&"); lock-free so channel detection is callable anywhere
	caseMappingAtomic     atomic.Pointer[string] // CASEMAPPING from ISUPPORT (e.g. "ascii"); lock-free so nick folding is callable anywhere
	supportsWHOX          bool                   // Server advertised the WHOX token in ISUPPORT (guarded by mu)
	supportsMonitor       bool                   // Server advertised the MONITOR token in ISUPPORT (guarded by mu)
	monitorLimit          int                    // MONITOR=<limit> from ISUPPORT; 0 = unlimited/unknown (guarded by mu)
	monitorStatus         map[string]bool        // MONITOR presence: lowercased nick -> online (guarded by monitorMu)
	monitorArmed          map[string]bool        // Nicks currently on the server MONITOR list (guarded by monitorMu)
	monitorMu             sync.Mutex             // Mutex for monitorStatus and monitorArmed
	whoisInProgress       map[string]*WhoisInfo  // Track WHOIS requests in progress (key: nickname)
	whoisMu               sync.Mutex             // Mutex for whoisInProgress map
	knownBots             map[string]bool        // Nicks recognized as IRCv3 bots this session (key: lowercased nick)
	knownBotsMu           sync.Mutex             // Mutex for knownBots map
	userMeta              map[string]*UserMeta   // Live roster attributes (away/account/host) this session (key: lowercased nick)
	userMetaMu            sync.Mutex             // Mutex for userMeta map
	autoJoinOnce          *sync.Once             // Guards the one auto-join per connection; re-created each Connect (guarded by mu)
	autoJoinAction        func()                 // What triggerAutoJoin runs once per connection; defaults to doAutoJoin (injectable for tests)
	enabledCaps           map[string]bool        // IRCv3 capabilities granted by the server
	chatHistoryMaxBatch   int                    // Max messages per CHATHISTORY request, from the chathistory=N cap value (0 = unknown, use default)
	capNegotiationDone    bool                   // Whether CAP negotiation has finished
	channelListItems      []ChannelListItem      // Temporary storage for LIST response
	channelListMu         sync.Mutex             // Mutex for channelListItems
	banLists              map[string][]BanEntry  // Per-channel ban entries collected between 367 and 368
	banListsMu            sync.Mutex             // Mutex for banLists
	rateLimiter           *RateLimiter           // Rate limiter for outgoing messages
	currentNick           string                 // Nick the server currently knows us by; differs from the preferred nick during a collision (guarded by mu)
	nickCollisionNotified bool                   // True once we've told the user (one time) that the preferred nick was unavailable (guarded by mu)
	pendingManualNick     string                 // Nick the user explicitly asked for via /nick and is awaiting; lets us surface a failure that the library's silent background reclaims would otherwise hide (guarded by mu)
	reconnecting          bool                   // True when this connection is an auto-reconnect after an unexpected drop (guarded by mu)
}

// ServerCapabilities stores parsed ISUPPORT information
type ServerCapabilities struct {
	Prefix       map[rune]rune // Map prefix char to mode char (e.g., '@' -> 'o', '+' -> 'v')
	PrefixString string        // Raw PREFIX string (e.g., "(ov)@+")
	ChanModes    string        // Raw CHANMODES string (e.g., "b,k,l,imnpst")
	ChanModesA   map[rune]bool // Type A modes (list modes, e.g. b)
	ChanModesB   map[rune]bool // Type B modes (always parameterized, e.g. k)
	ChanModesC   map[rune]bool // Type C modes (parameterized only when set, e.g. l)
	ChanModesD   map[rune]bool // Type D modes (boolean flags, e.g. imnpst)
	ChanTypes    string        // CHANTYPES token: channel-prefix characters (e.g. "#&"); empty means use the default
	CaseMapping  string        // CASEMAPPING token: nick/channel case-fold rule (e.g. "ascii", "rfc1459")
	UTF8Only     bool          // UTF8ONLY token present: server accepts only UTF-8 (ratified)
	ExtbanPrefix rune          // EXTBAN prefix char (e.g. '$'); 0 if none/unadvertised
	ExtbanTypes  map[rune]bool // EXTBAN type letters (e.g. 'a' for account-extban)
	Software     string        // Raw version token from RPL_MYINFO (004), e.g. "solanum-1.0"
	BotModeChar  rune          // BOT=<letter> ISUPPORT: the user mode that marks a bot (e.g. 'B'); 0 if unadvertised
	mu           sync.RWMutex  // Mutex for thread-safe access
}

// classification builds an immutable snapshot for the mode parser, combining the
// classified CHANMODES groups with the membership modes from PREFIX. When the server
// never advertised CHANMODES, RFC1459-style defaults are substituted so parsing still
// consumes parameters correctly. Callers should hold the client lock for reads.
func (sc *ServerCapabilities) classification() ModeClassification {
	a, b, c, d := sc.ChanModesA, sc.ChanModesB, sc.ChanModesC, sc.ChanModesD
	if len(a)+len(b)+len(c)+len(d) == 0 {
		a, b, c, d = classifyChanModes(defaultChanModes)
	}
	prefix := make(map[rune]bool, len(sc.Prefix))
	for _, modeLetter := range sc.Prefix {
		prefix[modeLetter] = true
	}
	return ModeClassification{List: a, Param: b, SetParam: c, Flag: d, Prefix: prefix}
}

// prefixForMode returns the display prefix char for a membership mode letter
// (e.g. 'o' -> '@'), and whether one exists. Callers should hold the client lock.
func (sc *ServerCapabilities) prefixForMode(modeLetter rune) (rune, bool) {
	for prefixChar, letter := range sc.Prefix {
		if letter == modeLetter {
			return prefixChar, true
		}
	}
	return 0, false
}

// prefixRank returns prefix chars ordered by descending privilege, derived from the
// order they appear in the PREFIX string (e.g. "(ov)@+" -> ['@','+']). Used to keep a
// user's stored prefix string sorted highest-first. Callers should hold the client lock.
func (sc *ServerCapabilities) prefixRank() []rune {
	if close := strings.IndexRune(sc.PrefixString, ')'); close >= 0 {
		return []rune(sc.PrefixString[close+1:])
	}
	return nil
}

// applyUserPrefix adds or removes the prefix char for the given membership mode letter
// in a user's stored prefix string, keeping it ordered highest-privilege first.
// Callers should hold the client lock.
func (sc *ServerCapabilities) applyUserPrefix(current string, modeLetter rune, add bool) string {
	prefixChar, ok := sc.prefixForMode(modeLetter)
	if !ok {
		return current
	}
	present := make(map[rune]bool)
	for _, r := range current {
		present[r] = true
	}
	if add {
		present[prefixChar] = true
	} else {
		delete(present, prefixChar)
	}

	var b strings.Builder
	for _, r := range sc.prefixRank() {
		if present[r] {
			b.WriteRune(r)
			delete(present, r)
		}
	}
	// Preserve any prefix chars not present in PREFIX rank (defensive for exotic servers).
	for r := range present {
		b.WriteRune(r)
	}
	return b.String()
}

// standardMembershipPrefixes is the conventional set of channel-membership prefix
// characters (owner, admin, op, halfop, voice). Used as a fallback when ISUPPORT
// PREFIX hasn't been parsed yet so NAMES parsing still works before 005 arrives.
const standardMembershipPrefixes = "~&@%+"

// splitMembershipPrefixes peels leading membership-prefix characters (e.g. "@+")
// off a NAMES (353) entry, returning the accumulated prefixes and the bare nick.
// With the multi-prefix capability the server sends every prefix a user holds,
// highest-privilege first, so the returned prefix order is preserved as received
// (no re-sorting). validPrefixes is the set of prefix chars advertised in
// ISUPPORT PREFIX; callers pass standardMembershipPrefixes as a fallback.
func splitMembershipPrefixes(entry, validPrefixes string) (modes, nick string) {
	i := 0
	for i < len(entry) && strings.IndexByte(validPrefixes, entry[i]) >= 0 {
		i++
	}
	return entry[:i], entry[i:]
}

// splitNickUserHost separates a NAMES (353) entry of the form "nick!user@host"
// into the bare nick and the "user@host" portion. With the userhost-in-names
// capability the server appends "!user@host" to each nick in NAMES; without it,
// entries are bare nicks. The membership prefixes must already have been peeled
// off. A missing "!" yields an empty userhost (the plain-NAMES case). Because
// "!" is not a valid nick character, splitting is safe whether or not the cap is
// active — its presence is itself the signal.
func splitNickUserHost(entry string) (nick, userhost string) {
	nick, userhost, _ = strings.Cut(entry, "!")
	return nick, userhost
}

// IsConnected returns whether the client is connected.
//
// This is a pure, side-effect-free read of the connection flag. Liveness
// detection (dead-connection / timeout handling) is owned entirely by the
// underlying ergochat/irc-go pingLoop, which PINGs on a KeepAlive interval and
// treats an unacked PING as fatal, firing the DisconnectCallback. That callback
// is the single authority that flips connected -> false. Keeping this method a
// plain getter avoids the old asymmetric latch where a passive staleness check
// could mark the connection dead while the live read loop kept delivering
// messages, leaving the UI permanently "disconnected" despite active traffic.
func (c *IRCClient) IsConnected() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.connected
}

// IsConnectedDirect is retained for its existing call sites and is now identical
// to IsConnected (both are pure reads of the connection flag).
func (c *IRCClient) IsConnectedDirect() bool {
	return c.IsConnected()
}

// ForceReconnect tears the connection down through the library's normal teardown
// path: conn.Quit() -> DisconnectCallback -> EventConnectionLost ->
// handleConnectionLost -> auto-reconnect. It is used by the system-wake hook to
// recover promptly after sleep, where the socket is usually dead but not yet
// detected by the library's ping loop. On a live connection that survived sleep
// this sends a clean QUIT and reconnects; on a dead one the library declares the
// QUIT unacknowledged after ConnectionReadTimeout and tears down. Liveness is the
// library's responsibility (its PING/PONG keepalive), not an app-side timestamp;
// this method only nudges a teardown, it never decides a connection is dead.
func (c *IRCClient) ForceReconnect() {
	c.mu.RLock()
	networkID := c.networkID
	connected := c.connected
	c.mu.RUnlock()
	if !connected {
		return
	}
	logger.Log.Info().Int64("network_id", networkID).Msg("Wake: forcing reconnect")
	c.conn.Quit()
}

// preferredNick is the nick the user configured and wants to hold. It matches
// the library's PreferredNick (conn.Nick is initialized from it) and is the one
// the library re-requests every keepalive while we're on an alternative.
func (c *IRCClient) preferredNick() string {
	return c.network.Nickname
}

// CurrentNick returns the nick the server currently knows us by, which can
// differ from the preferred nick while a collision is being resolved. Falls
// back to the preferred nick before registration has assigned one.
func (c *IRCClient) CurrentNick() string {
	c.mu.RLock()
	nick := c.currentNick
	c.mu.RUnlock()
	if nick == "" {
		return c.preferredNick()
	}
	return nick
}

// ChangeNick sends a user-initiated NICK request and records the requested nick
// so a resulting failure (e.g. ERR_NICKNAMEINUSE) is surfaced to the user rather
// than swallowed by the silence we keep for the library's background reclaims.
// The marker is recorded before the send to close any reply race, rolled back if
// nothing was sent, and cleared once the change succeeds (handleNickMessage) or
// the connection drops.
func (c *IRCClient) ChangeNick(nick string) error {
	c.mu.Lock()
	c.pendingManualNick = nick
	c.mu.Unlock()

	if err := c.SendRawCommand(fmt.Sprintf("NICK %s", nick)); err != nil {
		c.mu.Lock()
		if c.pendingManualNick == nick {
			c.pendingManualNick = ""
		}
		c.mu.Unlock()
		return err
	}
	return nil
}

// emitNickChanged notifies subscribers that our own nick changed, carrying both
// the new nick and the preferred nick so the UI can flag a pending reclaim.
func (c *IRCClient) emitNickChanged(nick string) {
	preferred := c.preferredNick()
	c.eventBus.Emit(events.Event{
		Type: EventNickChanged,
		Data: map[string]interface{}{
			"network":     c.network.Address,
			"networkId":   c.networkID,
			"nick":        nick,
			"desired":     preferred,
			"isPreferred": nick == preferred,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// nickErrorAttempt pulls the attempted nick out of a nick-error numeric. The
// common layout is "<client> <nick> :<reason>", so the nick is the second param;
// ERR_NONICKNAMEGIVEN (431) carries none, so this returns "".
func nickErrorAttempt(e ircmsg.Message) string {
	if len(e.Params) >= 2 {
		return e.Params[1]
	}
	return ""
}

// surfaceManualNickError reports a failed user-initiated nick change exactly
// once and returns whether it claimed the error. It fires only when the failing
// numeric answers the nick the user asked for via /nick — matched on the
// attempted nick, or unconditionally for numerics that carry none (431) so long
// as a request is pending. Clearing the marker means the library's later
// background reclaims of the same nick fall back to staying silent.
func (c *IRCClient) surfaceManualNickError(attempted, message string) bool {
	c.mu.Lock()
	manual := c.pendingManualNick != "" &&
		(attempted == "" || c.sameName(attempted, c.pendingManualNick))
	if manual {
		c.pendingManualNick = ""
	}
	c.mu.Unlock()

	if !manual {
		return false
	}
	c.storage.WriteMessageSync(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     message,
		MessageType: "status",
		Timestamp:   time.Now(),
	})
	return true
}

// handleNickInUse handles ERR_NICKNAMEINUSE (433). The library owns nick
// selection: before registration it cycles through alternatives on its own, and
// after registration its keepalive loop periodically re-requests the preferred
// nick (yielding a 433 each time it's still held). We therefore send no NICK of
// our own and surface only a single, one-time notice while still unregistered —
// the post-registration reclaim attempts stay silent so the log isn't spammed
// every keepalive interval. A 433 answering the user's explicit /nick is the
// exception: it's surfaced regardless of registration.
func (c *IRCClient) handleNickInUse(e ircmsg.Message) {
	attempted := nickErrorAttempt(e)
	if c.surfaceManualNickError(attempted, fmt.Sprintf("Couldn't change nick to %q — it's already in use.", attempted)) {
		return
	}

	c.mu.Lock()
	registered := c.currentNick != ""
	alreadyNotified := c.nickCollisionNotified
	if !registered {
		c.nickCollisionNotified = true
	}
	c.mu.Unlock()

	if registered || alreadyNotified {
		return
	}
	c.storage.WriteMessageSync(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     fmt.Sprintf("Nick %q is in use — trying alternatives…", c.preferredNick()),
		MessageType: "status",
		Timestamp:   time.Now(),
	})
}

// handleErroneousNick surfaces ERR_ERRONEUSNICKNAME (432) for a user-initiated
// /nick — the server rejected the nick as malformed (bad characters or length).
func (c *IRCClient) handleErroneousNick(e ircmsg.Message) {
	attempted := nickErrorAttempt(e)
	c.surfaceManualNickError(attempted, fmt.Sprintf("Couldn't change nick to %q — that nickname isn't allowed.", attempted))
}

// handleNickUnavailable surfaces ERR_UNAVAILRESOURCE (437) for a user-initiated
// /nick. Many networks hold a nick under "nick delay" for a while after its
// owner disconnects, so this is the usual answer when reclaiming right after a
// reconnect.
func (c *IRCClient) handleNickUnavailable(e ircmsg.Message) {
	attempted := nickErrorAttempt(e)
	c.surfaceManualNickError(attempted, fmt.Sprintf("Couldn't change nick to %q — it's temporarily unavailable (it may still be held from a recent disconnect).", attempted))
}

// handleNickCollision surfaces ERR_NICKCOLLISION (436) when it answers a
// user-initiated /nick. This is uncommon for a client request — it usually marks
// a server-side collision KILL — but if it does answer our attempt, say so.
func (c *IRCClient) handleNickCollision(e ircmsg.Message) {
	attempted := nickErrorAttempt(e)
	c.surfaceManualNickError(attempted, fmt.Sprintf("Couldn't change nick to %q — it collided with another user.", attempted))
}

// handleNoNickGiven surfaces ERR_NONICKNAMEGIVEN (431) for a user-initiated
// /nick. The numeric carries no nick, so it's attributed to any pending request.
func (c *IRCClient) handleNoNickGiven(_ ircmsg.Message) {
	c.surfaceManualNickError("", "Couldn't change nick — no nickname given.")
}

// handleWelcome records the nick the server actually assigned us on RPL_WELCOME
// (001). When it isn't our preferred nick, it explains that reclaiming will be
// retried automatically (the library does this on each keepalive).
func (c *IRCClient) handleWelcome(e ircmsg.Message) {
	if len(e.Params) < 1 {
		return
	}
	nick := e.Params[0]
	preferred := c.preferredNick()

	c.mu.Lock()
	c.currentNick = nick
	c.mu.Unlock()

	c.emitNickChanged(nick)

	if nick != preferred {
		c.storage.WriteMessageSync(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("Connected as %q. Cascade will keep re-requesting %q and switch to it automatically once it becomes free. To reclaim it now from a stuck session, use /ns regain %s.", nick, preferred, preferred),
			MessageType: "status",
			Timestamp:   time.Now(),
		})
	}
}

// handlePart processes an inbound PART: it removes the user from the channel's
// user list and, when WE are the one leaving, marks the channel closed so it
// drops out of the sidebar. Self-detection uses the live nick (isMe), so a PART
// arriving under a fallback nick after a collision is still recognized as ours.
func (c *IRCClient) handlePart(e ircmsg.Message) {
	channel := e.Params[0]
	user := e.Nick()
	reason := ""
	if len(e.Params) > 1 {
		reason = e.Params[1]
	}

	// Get channel and remove user from user list
	ch, err := c.storage.GetChannelByName(c.networkID, channel)
	channelUpdated := false
	if err == nil {
		if err := c.storage.RemoveChannelUser(ch.ID, user); err != nil {
			logger.Log.Error().Err(err).Str("user", user).Str("channel", channel).Msg("Failed to remove user from channel user list")
		} else {
			logger.Log.Debug().Str("user", user).Str("channel", channel).Msg("Removed user from channel user list")
			// Verify the write is committed by reading it back (forces WAL sync)
			// This ensures the user is immediately removed when the frontend queries
			_, _ = c.storage.GetChannelUsers(ch.ID)
		}

		// If the current user parted, mark channel as closed (CLOSED state)
		if c.isMe(user) {
			c.storage.UpdateChannelIsOpen(ch.ID, false)
			channelUpdated = true
		}

		// Store part message
		rawLine, _ := e.Line()
		partMsg := storage.Message{
			NetworkID: c.networkID,
			ChannelID: &ch.ID,
			User:      user,
			Message: fmt.Sprintf("%s left the channel%s", user, func() string {
				if reason != "" {
					return fmt.Sprintf(" (%s)", reason)
				}
				return ""
			}()),
			MessageType: "part",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		if err := c.storage.WriteMessageSync(partMsg); err != nil {
			// During shutdown, storage may be closed - this is expected
			if err.Error() == "storage is closed" {
				logger.Log.Debug().Msg("Skipping part message storage (storage closed during shutdown)")
			} else {
				logger.Log.Error().Err(err).Msg("Failed to store part message")
			}
		}
	}

	c.eventBus.Emit(events.Event{
		Type: EventUserParted,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"channel":   channel,
			"user":      user,
			"reason":    reason,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})

	// Emit channels changed event if our own part updated the channel state
	if channelUpdated {
		c.eventBus.Emit(events.Event{
			Type: EventChannelsChanged,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	}
}

// handleQuit processes an inbound QUIT: the user left the network entirely, so it
// drops their live roster metadata and removes them from every channel they were
// in, writing a quit line to each. Nick matching uses sameName (the server's
// advertised CASEMAPPING), so a QUIT whose source nick differs only in case —
// including the rfc1459 []\~ ↔ {}|^ pairing — still clears the stored roster
// entry rather than leaving a ghost behind.
func (c *IRCClient) handleQuit(e ircmsg.Message) {
	user := e.Nick()
	reason := ""
	if len(e.Params) > 0 {
		reason = e.Params[0]
	}

	// The user left the network entirely, so drop their live roster
	// attributes (away/account/host). PART/KICK deliberately don't do this.
	c.removeUserMeta(user)

	// Remove user from all channels they were in
	channels, err := c.storage.GetChannels(c.networkID)
	if err == nil {
		for _, ch := range channels {
			// Check if user is in this channel
			users, err := c.storage.GetChannelUsers(ch.ID)
			if err == nil {
				for _, u := range users {
					if c.sameName(u.Nickname, user) {
						// User is in this channel, remove them
						if err := c.storage.RemoveChannelUser(ch.ID, user); err != nil {
							logger.Log.Error().Err(err).Str("user", user).Str("channel", ch.Name).Msg("Failed to remove user from channel user list")
						} else {
							logger.Log.Debug().Str("user", user).Str("channel", ch.Name).Msg("Removed user from channel user list")
							// Verify the write is committed by reading it back (forces WAL sync)
							// This ensures the user is immediately removed when the frontend queries
							_, _ = c.storage.GetChannelUsers(ch.ID)
						}

						// Store quit message in the channel
						rawLine, _ := e.Line()
						quitMsg := storage.Message{
							NetworkID: c.networkID,
							ChannelID: &ch.ID,
							User:      user,
							Message: fmt.Sprintf("%s quit%s", user, func() string {
								if reason != "" {
									return fmt.Sprintf(" (%s)", reason)
								}
								return ""
							}()),
							MessageType: "quit",
							Timestamp:   time.Now(),
							RawLine:     rawLine,
						}
						if err := c.storage.WriteMessageSync(quitMsg); err != nil {
							// During shutdown, storage may be closed - this is expected
							if err.Error() == "storage is closed" {
								logger.Log.Debug().Msg("Skipping quit message storage (storage closed during shutdown)")
							} else {
								logger.Log.Error().Err(err).Msg("Failed to store quit message")
							}
						}
						break
					}
				}
			}
		}
	}

	c.eventBus.Emit(events.Event{
		Type: EventUserQuit,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"user":      user,
			"reason":    reason,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// handleForwardedJoin processes ERR_LINKCHANNEL (470): the server redirected a
// JOIN from one channel to another (Libera's +f channel forwarding, e.g. when we
// are banned or quieted on the requested channel). The requested channel was
// never actually joined, so its buffer must be closed to keep the sidebar
// faithful to real membership — otherwise it lingers showing the requested name
// (e.g. "#chat") while we are really only in the forwarded-to channel
// ("##chat-overflow"). The forwarded-to channel opens via its own JOIN. A status
// line records the redirect so the change is not silent.
func (c *IRCClient) handleForwardedJoin(e ircmsg.Message) {
	if len(e.Params) < 3 {
		return
	}
	from := e.Params[1]
	to := e.Params[2]

	if ch, err := c.storage.GetChannelByName(c.networkID, from); err == nil {
		c.storage.UpdateChannelIsOpen(ch.ID, false)
		c.eventBus.Emit(events.Event{
			Type: EventChannelsChanged,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	}

	if err := c.storage.WriteMessageSync(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil, // status window
		User:        "*",
		Message:     fmt.Sprintf("%s is forwarding new joins to %s — you were redirected there.", from, to),
		MessageType: "status",
		Timestamp:   time.Now(),
	}); err != nil {
		logger.Log.Debug().Err(err).Msg("Failed to store channel-forward status message")
	}
}

// handleNickMessage handles inbound NICK changes. It always updates channel user
// lists and notifies subscribers (for anyone). When the change is our own —
// detected because the old nick matches the nick the server currently knows us
// by — it tracks the new nick and, if we've reclaimed our preferred nick, says
// so.
func (c *IRCClient) handleNickMessage(e ircmsg.Message) {
	oldNick := e.Nick()
	newNick := e.Params[0]

	// Update nickname in all channels for this network
	if err := c.storage.UpdateChannelUserNickname(c.networkID, oldNick, newNick); err != nil {
		logger.Log.Error().Err(err).Str("oldNick", oldNick).Str("newNick", newNick).Msg("Failed to update nickname in channel user lists")
	} else {
		logger.Log.Debug().Str("oldNick", oldNick).Str("newNick", newNick).Msg("Updated nickname in channel user lists")
	}

	// Carry live roster attributes (away/account/host) over to the new nick so
	// badges and dimming don't go stale on a rename.
	c.renameUserMeta(oldNick, newNick)

	c.eventBus.Emit(events.Event{
		Type: EventUserNick,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"oldNick":   oldNick,
			"newNick":   newNick,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})

	// Detect when the change is our own and keep our tracked nick in sync.
	c.mu.RLock()
	isSelf := c.sameName(oldNick, c.currentNick)
	c.mu.RUnlock()
	if !isSelf {
		return
	}

	preferred := c.preferredNick()
	reclaimed := c.sameName(newNick, preferred)
	c.mu.Lock()
	c.currentNick = newNick
	// Our nick changed successfully, so any manual /nick request is now resolved.
	c.pendingManualNick = ""
	if reclaimed {
		// Reclaimed the preferred nick; allow a future collision to notify afresh.
		c.nickCollisionNotified = false
	}
	c.mu.Unlock()

	c.emitNickChanged(newNick)

	if reclaimed {
		c.storage.WriteMessageSync(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("Reclaimed your preferred nick %q.", preferred),
			MessageType: "status",
			Timestamp:   time.Now(),
		})
	}
}

// NewIRCClient creates a new IRC client
func NewIRCClient(network *storage.Network, eventBus *events.EventBus, storage *storage.Storage) *IRCClient {
	var saslMechanism, saslUsername, saslPassword string
	if network.SASLMechanism != nil {
		saslMechanism = *network.SASLMechanism
	}
	if network.SASLUsername != nil {
		saslUsername = *network.SASLUsername
	}
	if network.SASLPassword != nil {
		saslPassword = *network.SASLPassword
	}

	client := &IRCClient{
		network:             network,
		eventBus:            eventBus,
		storage:             storage,
		saslEnabled:         network.SASLEnabled,
		saslMechanism:       saslMechanism,
		saslUsername:        saslUsername,
		saslPassword:        saslPassword,
		saslInProgress:      false,
		saslAuthenticated:   false,
		saslCapRequested:    false,
		saslCapAcknowledged: false,
		namesInProgress:     make(map[string]bool),
		whoisInProgress:     make(map[string]*WhoisInfo),
		knownBots:           make(map[string]bool),
		userMeta:            make(map[string]*UserMeta),
		monitorStatus:       make(map[string]bool),
		monitorArmed:        make(map[string]bool),
		autoJoinOnce:        &sync.Once{},
		enabledCaps:         make(map[string]bool),
		banLists:            make(map[string][]BanEntry),
		serverCapabilities: &ServerCapabilities{
			Prefix:       make(map[rune]rune),
			PrefixString: "",
			ChanModes:    "",
		},
		rateLimiter: NewRateLimiter(4, 2*time.Second),
	}

	// Debug: Log SASL configuration
	if network.SASLEnabled {
		logger.Log.Debug().
			Str("network", network.Name).
			Str("mechanism", saslMechanism).
			Str("username", saslUsername).
			Msg("SASL enabled")
	} else {
		logger.Log.Debug().Str("network", network.Name).Msg("SASL disabled")
	}

	// Create ircevent connection
	client.conn = &ircevent.Connection{
		Server:        fmt.Sprintf("%s:%d", network.Address, network.Port),
		Nick:          network.Nickname,
		User:          network.Username,
		RealName:      network.Realname,
		UseTLS:        network.TLS,
		Password:      network.Password,
		ReconnectFreq: 0, // Disable automatic reconnection - we'll handle it manually
		// Liveness detection is owned by the library's pingLoop: it sends a
		// keepalive PING every KeepAlive and treats an unacked PING (within
		// Timeout, enforced via socket read/write deadlines) as fatal, firing
		// the DisconnectCallback. This is the single source of truth for whether
		// the connection is alive, including across OS sleep/wake. Constraint:
		// KeepAlive must be >= Timeout.
		Timeout:   constants.ConnectionReadTimeout,
		KeepAlive: constants.ConnectionKeepAlive,
	}

	// Auto-join runs once per connection through triggerAutoJoin; doAutoJoin is the
	// real action (overridable in tests, which have no live connection to JOIN on).
	client.autoJoinAction = client.doAutoJoin

	// Set up event handlers
	client.setupHandlers()

	return client
}

// markBot records that a nick is an IRCv3 bot (learned from the `bot` message tag
// or RPL_WHOISBOT). It is idempotent: the EventBotDetected event is emitted only
// on first discovery so repeated bot messages do not spam the frontend. Nicks are
// stored lowercased so lookups are case-insensitive (IRC nicks are case-folding).
func (c *IRCClient) markBot(nick string) {
	if nick == "" {
		return
	}
	key := strings.ToLower(nick)

	c.knownBotsMu.Lock()
	if c.knownBots[key] {
		c.knownBotsMu.Unlock()
		return
	}
	c.knownBots[key] = true
	c.knownBotsMu.Unlock()

	c.eventBus.Emit(events.Event{
		Type: EventBotDetected,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"nickname":  key,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// markSelfBotFromUserMode handles a user-mode MODE line targeting ourselves. If
// it ADDS the advertised bot letter to our own nick, we mark ourselves via the
// shared markBot path so the self nick shows the bot badge. All other user-mode
// changes are intentionally ignored (we track no general self-usermode state).
func (c *IRCClient) markSelfBotFromUserMode(target, modeStr string) {
	if !c.sameName(target, c.CurrentNick()) {
		return
	}
	c.mu.RLock()
	botChar := c.serverCapabilities.BotModeChar
	c.mu.RUnlock()
	if botChar == 0 {
		return
	}
	adding := true
	for _, r := range modeStr {
		switch r {
		case '+':
			adding = true
		case '-':
			adding = false
		case botChar:
			if adding {
				c.markBot(c.CurrentNick())
			}
		}
	}
}

// maybeMarkBotFromTag recognizes the sender as a bot when an incoming message
// carries the IRCv3 `bot` tag. Per spec the tag is valueless and any value is
// ignored, so we only check presence. Shared by the PRIVMSG, NOTICE, and JOIN
// handlers.
func (c *IRCClient) maybeMarkBotFromTag(e ircmsg.Message) {
	if present, _ := e.GetTag("bot"); present {
		c.markBot(e.Nick())
	}
}

// handleWhoisBot processes RPL_WHOISBOT (335): it flags the in-progress WHOIS as
// a bot and records the nick in the session bot set. nick is e.Params[1].
func (c *IRCClient) handleWhoisBot(e ircmsg.Message) {
	if len(e.Params) < 2 {
		return
	}
	nickname := e.Params[1]

	c.whoisMu.Lock()
	if c.whoisInProgress[nickname] == nil {
		c.whoisInProgress[nickname] = &WhoisInfo{
			Nickname: nickname,
			Network:  c.network.Address,
		}
	}
	c.whoisInProgress[nickname].IsBot = true
	c.whoisMu.Unlock()

	c.markBot(nickname)
}

// BotNicks returns the lowercased nicks recognized as bots this session. Used by
// the App layer to hydrate the frontend when a window opens or reloads.
func (c *IRCClient) BotNicks() []string {
	c.knownBotsMu.Lock()
	defer c.knownBotsMu.Unlock()
	nicks := make([]string, 0, len(c.knownBots))
	for nick := range c.knownBots {
		nicks = append(nicks, nick)
	}
	return nicks
}

// applyUserMeta updates the live roster attributes for a nick. It clones the
// current value, runs mutate against the clone, and only stores + emits
// EventUserMetaChanged when the result actually differs from what we had — so a
// redundant AWAY/ACCOUNT/account-tag never spams the frontend (mirrors markBot's
// idempotency). Nicks are keyed lowercased since IRC nicks are case-folding.
func (c *IRCClient) applyUserMeta(nick string, mutate func(*UserMeta)) {
	if nick == "" {
		return
	}
	key := strings.ToLower(nick)

	c.userMetaMu.Lock()
	current := UserMeta{}
	if existing := c.userMeta[key]; existing != nil {
		current = *existing
	}
	updated := current
	mutate(&updated)
	if updated == current {
		c.userMetaMu.Unlock()
		return
	}
	stored := updated
	c.userMeta[key] = &stored
	c.userMetaMu.Unlock()

	c.emitUserMeta(key, stored)
}

// emitUserMeta publishes a snapshot of a nick's roster attributes. nick must
// already be lowercased.
func (c *IRCClient) emitUserMeta(nick string, meta UserMeta) {
	c.eventBus.Emit(events.Event{
		Type: EventUserMetaChanged,
		Data: map[string]interface{}{
			"network":      c.network.Address,
			"networkId":    c.networkID,
			"nickname":     nick,
			"away":         meta.Away,
			"away_message": meta.AwayMessage,
			"account":      meta.Account,
			"host":         meta.Host,
			"realname":     meta.Realname,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// renameUserMeta moves a nick's roster attributes to its new nick on NICK so the
// away/account/host follow the user instead of going stale. Emits for the new
// nick when anything moved.
func (c *IRCClient) renameUserMeta(oldNick, newNick string) {
	if oldNick == "" || newNick == "" {
		return
	}
	oldKey := strings.ToLower(oldNick)
	newKey := strings.ToLower(newNick)
	if oldKey == newKey {
		return
	}

	c.userMetaMu.Lock()
	meta := c.userMeta[oldKey]
	if meta == nil {
		c.userMetaMu.Unlock()
		return
	}
	delete(c.userMeta, oldKey)
	moved := *meta
	c.userMeta[newKey] = &moved
	c.userMetaMu.Unlock()

	c.emitUserMeta(newKey, moved)
}

// removeUserMeta drops a nick's roster attributes when the user leaves the
// network (QUIT). PART/KICK do not call this: the user may remain in other
// channels, and away/account/host are network-wide, not channel-scoped.
func (c *IRCClient) removeUserMeta(nick string) {
	if nick == "" {
		return
	}
	c.userMetaMu.Lock()
	delete(c.userMeta, strings.ToLower(nick))
	c.userMetaMu.Unlock()
}

// handleAway processes away-notify: ":nick AWAY :message" marks the user away,
// ":nick AWAY" (no param, or empty) marks them back.
func (c *IRCClient) handleAway(e ircmsg.Message) {
	message := ""
	if len(e.Params) > 0 {
		message = e.Params[0]
	}
	away := message != ""
	c.applyUserMeta(e.Nick(), func(m *UserMeta) {
		m.Away = away
		m.AwayMessage = message
	})
}

// handleAccount processes account-notify: ":nick ACCOUNT <account>" where "*"
// means the user logged out.
func (c *IRCClient) handleAccount(e ircmsg.Message) {
	if len(e.Params) < 1 {
		return
	}
	account := e.Params[0]
	if account == "*" {
		account = ""
	}
	c.applyUserMeta(e.Nick(), func(m *UserMeta) { m.Account = account })
}

// handleChghost processes chghost: ":nick CHGHOST <newuser> <newhost>" — the
// user's ident/host changed mid-session.
func (c *IRCClient) handleChghost(e ircmsg.Message) {
	if len(e.Params) < 2 {
		return
	}
	host := e.Params[0] + "@" + e.Params[1]
	c.applyUserMeta(e.Nick(), func(m *UserMeta) { m.Host = host })
}

// maybeApplyExtendedJoin records the joiner's account and realname from an
// extended-join JOIN. When that cap is negotiated, JOIN carries the account as
// the 2nd param ("*" = not logged in) and the realname as the 3rd. No-op on a
// plain JOIN or when the cap isn't enabled.
func (c *IRCClient) maybeApplyExtendedJoin(e ircmsg.Message) {
	if !c.capEnabled("extended-join") || len(e.Params) < 2 {
		return
	}
	account := e.Params[1]
	if account == "*" {
		account = ""
	}
	realname := ""
	if len(e.Params) >= 3 {
		realname = e.Params[2]
	}
	c.applyUserMeta(e.Nick(), func(m *UserMeta) {
		m.Account = account
		if realname != "" {
			m.Realname = realname
		}
	})
}

// handleInvite routes an inbound INVITE. An invite addressed to us is emitted as
// EventInviteReceived for the in-memory Invites inbox (App owns trust/notify).
// The invite-notify "ops FYI" form (a third party invited to a channel we
// operate) is informational only and stays a plain status line.
func (c *IRCClient) handleInvite(e ircmsg.Message) {
	if len(e.Params) < 2 {
		return
	}
	target := e.Params[0]
	channel := e.Params[1]
	inviter := e.Nick()

	if !c.sameName(target, c.CurrentNick()) {
		c.writeStatusLine("status", fmt.Sprintf("%s invited %s to %s", inviter, target, channel))
		return
	}

	c.eventBus.Emit(events.Event{
		Type: EventInviteReceived,
		Data: map[string]interface{}{
			"networkId":  c.networkID,
			"inviter":    inviter,
			"channel":    channel,
			"receivedAt": time.Now().Format(time.RFC3339),
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// handleInviting surfaces RPL_INVITING (341) as a status confirmation after we
// send an INVITE. Params: "<me> <nick> <channel>".
func (c *IRCClient) handleInviting(e ircmsg.Message) {
	if len(e.Params) < 3 {
		return
	}
	c.writeStatusLine("status", fmt.Sprintf("Invited %s to %s", e.Params[1], e.Params[2]))
}

// handleUserOnChannel surfaces ERR_USERONCHANNEL (443) when the target of an
// INVITE is already on the channel. Params: "<me> <nick> <channel> :<reason>".
func (c *IRCClient) handleUserOnChannel(e ircmsg.Message) {
	if len(e.Params) < 3 {
		return
	}
	c.writeStatusLine("status", fmt.Sprintf("%s is already on %s", e.Params[1], e.Params[2]))
}

// writeStatusLine writes a line to the network status buffer and refreshes it
// live. The sync write commits the row before the event fires (avoiding a
// write/notify race), and channel:nil routes the refresh to the status buffer
// (see the message-event handler in App.tsx).
func (c *IRCClient) writeStatusLine(messageType, text string) {
	if err := c.storage.WriteMessageSync(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     text,
		MessageType: messageType,
		Timestamp:   time.Now(),
	}); err != nil {
		logger.Log.Warn().Err(err).Str("type", messageType).Msg("Failed to write status line")
	}
	c.eventBus.Emit(events.Event{
		Type: EventMessageReceived,
		Data: map[string]interface{}{
			"network":     c.network.Address,
			"networkId":   c.networkID,
			"channel":     nil,
			"user":        "*",
			"message":     text,
			"messageType": "privmsg",
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// writeChannelSystemLine writes an informational system line into a specific
// channel's buffer (rather than the network status buffer) and refreshes it
// live. Used for server numerics that describe a channel we're in — e.g.
// RPL_TOPICWHOTIME (333) / RPL_NOTOPIC (331). If we aren't tracking the channel,
// the line falls back to the status buffer so it is never dropped.
func (c *IRCClient) writeChannelSystemLine(channel, messageType, text string) {
	ch, err := c.storage.GetChannelByName(c.networkID, channel)
	if err != nil {
		c.writeStatusLine(messageType, text)
		return
	}
	if err := c.storage.WriteMessageSync(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   &ch.ID,
		User:        "*",
		Message:     text,
		MessageType: messageType,
		Timestamp:   time.Now(),
	}); err != nil {
		logger.Log.Warn().Err(err).Str("channel", channel).Str("type", messageType).Msg("Failed to write channel system line")
	}
	c.eventBus.Emit(events.Event{
		Type: EventMessageReceived,
		Data: map[string]interface{}{
			"network":     c.network.Address,
			"networkId":   c.networkID,
			"channel":     channel,
			"user":        "*",
			"message":     text,
			"messageType": "privmsg",
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// handleStandardReply surfaces an IRCv3 standard reply (FAIL / WARN / NOTE) in
// the status buffer. The wire form is
//
//	<FAIL|WARN|NOTE> <command> <code> [<context>...] :<description>
//
// FAIL maps to an error line, WARN to a warning line, NOTE to a status line.
func (c *IRCClient) handleStandardReply(e ircmsg.Message, messageType string) {
	if len(e.Params) < 2 {
		return
	}
	command := e.Params[0]
	description := e.Params[len(e.Params)-1]
	code := ""
	if len(e.Params) >= 3 {
		code = e.Params[1]
	}
	var text string
	if code != "" {
		text = fmt.Sprintf("%s %s (%s): %s", e.Command, command, code, description)
	} else {
		text = fmt.Sprintf("%s %s: %s", e.Command, command, description)
	}
	c.writeStatusLine(messageType, text)
}

// genericErrorNumerics are server error replies that carry a human-readable
// description but have no dedicated handler. ergochat/irc-go drops any numeric
// without a registered callback silently (it rejects wildcard callbacks), so
// without this a failed PM (401 ERR_NOSUCHNICK), a blocked channel send
// (404 ERR_CANNOTSENDTOCHAN), or a typo'd command (421 ERR_UNKNOWNCOMMAND) would
// give the user no feedback at all. Numerics that already have purpose-built
// handlers (join errors 470–477, nick errors 431–437, 481/482, SASL 90x, …) are
// intentionally excluded so they aren't shown twice.
var genericErrorNumerics = []string{
	"401", // ERR_NOSUCHNICK
	"402", // ERR_NOSUCHSERVER
	"403", // ERR_NOSUCHCHANNEL
	"404", // ERR_CANNOTSENDTOCHAN
	"405", // ERR_TOOMANYCHANNELS
	"406", // ERR_WASNOSUCHNICK
	"407", // ERR_TOOMANYTARGETS
	"408", // ERR_NOSUCHSERVICE
	"409", // ERR_NOORIGIN
	"411", // ERR_NORECIPIENT
	"412", // ERR_NOTEXTTOSEND
	"413", // ERR_NOTOPLEVEL
	"414", // ERR_WILDTOPLEVEL
	"415", // ERR_BADMASK
	"421", // ERR_UNKNOWNCOMMAND
	"423", // ERR_NOADMININFO
	"424", // ERR_FILEERROR
	"441", // ERR_USERNOTINCHANNEL
	"444", // ERR_NOLOGIN
	"451", // ERR_NOTREGISTERED
	"461", // ERR_NEEDMOREPARAMS
	"462", // ERR_ALREADYREGISTERED
	"463", // ERR_NOPERMFORHOST
	"464", // ERR_PASSWDMISMATCH
	"465", // ERR_YOUREBANNEDCREEP
	"466", // ERR_YOUWILLBEBANNED
	"467", // ERR_KEYSET
	"472", // ERR_UNKNOWNMODE
	"476", // ERR_BADCHANMASK
	"478", // ERR_BANLISTFULL
	"479", // ERR_BADCHANNAME
	"480", // ERR_CANNOTKNOCK / ERR_THROTTLE
	"483", // ERR_CANTKILLSERVER
	"484", // ERR_RESTRICTED
	"485", // ERR_UNIQOPPRIVSNEEDED
	"491", // ERR_NOOPERHOST
	"501", // ERR_UMODEUNKNOWNFLAG
	"502", // ERR_USERSDONTMATCH
	"524", // ERR_HELPNOTFOUND
	"525", // ERR_INVALIDKEY
	"723", // ERR_NOPRIVS
}

// handleServerNumeric surfaces a generic server error reply (see
// genericErrorNumerics) as an error line in the status buffer. The wire form is
// "<numeric> <me> [<subject>...] :<description>"; formatServerNumeric renders the
// subject(s) and description into one line. Without this the reply is dropped.
func (c *IRCClient) handleServerNumeric(e ircmsg.Message) {
	if text := formatServerNumeric(e.Params); text != "" {
		c.writeStatusLine("error", text)
	}
}

// handleSetname processes the setname capability: ":nick SETNAME :new real name"
// announces that a user changed their realname mid-session. The new realname is
// the (possibly multi-word) trailing parameter.
func (c *IRCClient) handleSetname(e ircmsg.Message) {
	if len(e.Params) < 1 {
		return
	}
	realname := e.Params[0]
	c.applyUserMeta(e.Nick(), func(m *UserMeta) { m.Realname = realname })
}

// maybeApplyAccountTag records the sender's account when an incoming message
// carries the IRCv3 `@account` tag (account-tag cap). An empty value or "*"
// means the sender is not logged in. Shared by the PRIVMSG, NOTICE, and JOIN
// handlers, alongside maybeMarkBotFromTag.
func (c *IRCClient) maybeApplyAccountTag(e ircmsg.Message) {
	present, account := e.GetTag("account")
	if !present {
		return
	}
	if account == "*" {
		account = ""
	}
	c.applyUserMeta(e.Nick(), func(m *UserMeta) { m.Account = account })
}

// UserMetaFor returns a copy of the roster attributes known for a nick this
// session, and whether any were recorded.
func (c *IRCClient) UserMetaFor(nick string) (UserMeta, bool) {
	c.userMetaMu.Lock()
	defer c.userMetaMu.Unlock()
	if m := c.userMeta[strings.ToLower(nick)]; m != nil {
		return *m, true
	}
	return UserMeta{}, false
}

// AllUserMeta returns a snapshot of every nick's roster attributes this session,
// keyed by lowercased nick. Used by the App layer to hydrate the frontend when a
// window opens or reloads; live changes arrive via EventUserMetaChanged.
func (c *IRCClient) AllUserMeta() map[string]UserMeta {
	c.userMetaMu.Lock()
	defer c.userMetaMu.Unlock()
	out := make(map[string]UserMeta, len(c.userMeta))
	for nick, m := range c.userMeta {
		out[nick] = *m
	}
	return out
}

// accountFor returns the logged-in account for nick from the live roster, or ""
// if unknown / not logged in.
func (c *IRCClient) accountFor(nick string) string {
	c.userMetaMu.Lock()
	defer c.userMetaMu.Unlock()
	if m, ok := c.userMeta[strings.ToLower(nick)]; ok && m != nil {
		return m.Account
	}
	return ""
}

// handlePrivmsg is the PRIVMSG callback. It is extracted from setupHandlers so
// tests can drive the real production path without a live connection.
func (c *IRCClient) handlePrivmsg(e ircmsg.Message) {
	channel := e.Params[0]
	message := e.Params[1]
	user := e.Nick()

	// IRCv3 bot mode: servers tag messages from bots with a valueless `bot`
	// tag. Recognize the sender as a bot (covers plain PRIVMSG and CTCP ACTION).
	c.maybeMarkBotFromTag(e)
	// account-tag: learn the sender's account from the `@account` tag.
	c.maybeApplyAccountTag(e)

	// Check if this is a CTCP message (wrapped in \001)
	if len(message) >= 2 && message[0] == '\001' && message[len(message)-1] == '\001' {
		ctcpMessage := message[1 : len(message)-1] // Remove \001 delimiters
		parts := strings.Fields(ctcpMessage)
		if len(parts) > 0 {
			ctcpCommand := strings.ToUpper(parts[0])
			ctcpArgs := ""
			if len(parts) > 1 {
				ctcpArgs = strings.Join(parts[1:], " ")
			}

			// Handle CTCP requests
			if ctcpCommand == "ACTION" {
				// CTCP ACTION - already handled, but store as action type
				// Determine if it's a channel or private message
				var channelID *int64
				var pmTarget string
				if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
					ch, err := c.storage.GetChannelByName(c.networkID, channel)
					if err == nil {
						channelID = &ch.ID
					}
				} else {
					// Private message - create or get PM conversation keyed by the peer
					pmTarget = c.pmPeer(user, channel)
					_, err := c.storage.GetOrCreatePMConversation(c.networkID, pmTarget, c.network.Nickname)
					if err != nil {
						logger.Log.Error().Err(err).Str("user", user).Str("pmTarget", pmTarget).Msg("Failed to create/get PM conversation")
					}
				}

				rawLine, _ := e.Line()
				msg := storage.Message{
					NetworkID:      c.networkID,
					ChannelID:      channelID,
					User:           user,
					Message:        fmt.Sprintf("* %s %s", user, ctcpArgs),
					MessageType:    "action",
					Timestamp:      c.getMessageTime(e),
					RawLine:        rawLine,
					PMTarget:       pmTarget,
					MsgID:          c.getMsgID(e),
					ReplyMsgID:     c.getReplyTag(e),
					ChannelContext: c.getChannelContext(e),
				}
				c.storage.WriteMessageSync(msg)
				c.eventBus.Emit(events.Event{
					Type: EventMessageReceived,
					Data: map[string]interface{}{
						"network":     c.network.Address,
						"networkId":   c.networkID,
						"networkName": c.network.Name,
						"channel":     channel,
						"user":        user,
						"message":     ctcpArgs,
						"account":     c.accountFor(user),
						"msgid":       c.getMsgID(e),
						"messageUnix": c.getMessageTime(e).Unix(),
						"isAction":    true,
					},
					Timestamp: time.Now(),
					Source:    events.EventSourceIRC,
				})
			} else {
				// Other CTCP requests - send response
				c.handleCTCPRequest(user, ctcpCommand, ctcpArgs)
				// Don't store CTCP requests as regular messages
				return
			}
		}
		return
	}

	// Regular PRIVMSG (not CTCP)
	// Handle echo-message: when the cap is enabled, the server echoes back our
	// sent messages as the canonical copy. We store them here and skip storage
	// in SendMessage. When echo-message is NOT enabled, drop self-to-self echoes.
	if c.isMe(user) {
		if !c.isEchoMessage(e) {
			// echo-message not enabled; this is a legacy self-echo - skip it
			if c.isMe(channel) {
				return
			}
		}
		// With echo-message enabled, fall through to store the server echo
	}

	// Determine if it's a channel or private message
	var channelID *int64
	var pmTarget string
	if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
		// Channel message - look up channel ID
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err == nil {
			channelID = &ch.ID
		} else {
			// Channel not found in database - might be a channel we haven't joined yet
			// Store with nil channel_id for now
			logger.Log.Debug().Err(err).Str("channel", channel).Msg("Channel not found in database")
		}
	} else {
		// Private message - create or get PM conversation keyed by the peer
		pmTarget = c.pmPeer(user, channel)
		_, err := c.storage.GetOrCreatePMConversation(c.networkID, pmTarget, c.network.Nickname)
		if err != nil {
			logger.Log.Error().Err(err).Str("user", user).Str("pmTarget", pmTarget).Msg("Failed to create/get PM conversation")
		}
	}

	rawLine, _ := e.Line()

	msg := storage.Message{
		NetworkID:      c.networkID,
		ChannelID:      channelID,
		User:           user,
		Message:        message,
		MessageType:    "privmsg",
		Timestamp:      c.getMessageTime(e),
		RawLine:        rawLine,
		PMTarget:       pmTarget,
		MsgID:          c.getMsgID(e),
		ReplyMsgID:     c.getReplyTag(e),
		ChannelContext: c.getChannelContext(e),
	}

	// Store message (use sync write so it appears immediately)
	if err := c.storage.WriteMessageSync(msg); err != nil {
		c.eventBus.Emit(events.Event{
			Type:      EventError,
			Data:      map[string]interface{}{"error": err.Error()},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	}

	// Emit event. pmTarget carries the conversation peer for DMs (empty for
	// channel messages) so the frontend can route the event to the open
	// "pm:<peer>" pane — the raw `channel` is our own nick for inbound DMs and
	// never matches that pane on its own.
	c.eventBus.Emit(events.Event{
		Type: EventMessageReceived,
		Data: map[string]interface{}{
			"network":        c.network.Address,
			"networkId":      c.networkID,
			"networkName":    c.network.Name,
			"channel":        channel,
			"user":           user,
			"message":        message,
			"account":        c.accountFor(user),
			"msgid":          c.getMsgID(e),
			"messageUnix":    c.getMessageTime(e).Unix(),
			"isAction":       false,
			"pmTarget":       pmTarget,
			"messageType":    msg.MessageType,
			"replyMsgid":     c.getReplyTag(e),
			"channelContext": c.getChannelContext(e),
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// onConnect is the library connect callback: it marks the client connected,
// records the status line, kicks off IRCv3 capability negotiation, and announces
// EventConnectionEstablished.
func (c *IRCClient) onConnect(e ircmsg.Message) {
	c.mu.Lock()
	abandoned := c.abandoned
	if !abandoned {
		c.connected = true
	}
	c.mu.Unlock()

	// Ghost guard: we already signalled quit on this client, but the library Loop
	// fired one last reconnect anyway (it doesn't re-check its quit flag after the
	// reconnect delay). Refuse to participate — don't mark connected, negotiate
	// CAP, write status, or announce Established. Re-issue Quit so the socket the
	// Loop just opened is torn down promptly instead of lingering until the next
	// iteration; the Loop then sees the quit flag and exits for good.
	if abandoned {
		c.signalQuit("Disconnecting")
		return
	}

	// Store connection message in status window
	rawLine, _ := e.Line()
	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil, // Status window
		User:        "*",
		Message:     "Connected to server",
		MessageType: "status",
		Timestamp:   time.Now(),
		RawLine:     rawLine,
	}
	c.storage.WriteMessage(statusMsg)

	// Always start IRCv3 capability negotiation
	c.storage.WriteMessage(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     "Starting IRCv3 capability negotiation (CAP LS 302)",
		MessageType: "status",
		Timestamp:   time.Now(),
	})
	c.conn.SendRaw("CAP LS 302")

	// On connection established, restore channel state after restart
	// Clear all channel_users entries since we're not actually joined anymore
	if err := c.storage.ClearNetworkChannelUsers(c.networkID); err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to clear channel users on connection")
	} else {
		logger.Log.Debug().Msg("Cleared channel users on connection (restoring state after restart)")
	}

	c.eventBus.Emit(events.Event{
		Type:      EventConnectionEstablished,
		Data:      map[string]interface{}{"network": c.network.Address, "networkId": c.networkID},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// onDisconnect is the library disconnect callback: it marks the client
// disconnected, records "Disconnected" in every open channel and the status
// window, clears stale user lists, and announces EventConnectionLost (which the
// app turns into an auto-reconnect with a fresh client).
func (c *IRCClient) onDisconnect(e ircmsg.Message) {
	c.mu.Lock()
	c.connected = false
	// The app is the sole reconnect owner: every drop is recovered by building a
	// FRESH IRCClient (handleConnectionLost -> connectNetwork), never by reusing
	// this object. So retire this one the instant it drops. The library Loop will
	// try to reconnect it on its own — immediately, for a long-lived link whose
	// reconnect delay has already elapsed — but this callback provably runs to
	// completion before that reconnect (ergochat's readLoop fires disconnect
	// callbacks in a defer that precedes wg.Done(), and Loop waits on wg before
	// re-dialing). Setting abandoned here therefore closes the race that a later
	// signalQuit could not: the Loop's reconnect lands in onConnect as a no-op
	// instead of a ghost that steals the nick and reports itself connected.
	c.abandoned = true
	// Reset nick-collision state so a reconnect starts from a clean slate.
	c.currentNick = ""
	c.nickCollisionNotified = false
	c.pendingManualNick = ""
	c.mu.Unlock()

	// Write "Disconnected" messages to all open/joined channels BEFORE clearing users
	// This ensures we can still identify which channels the user was in
	// Do this SYNCHRONOUSLY so it completes before the process can exit
	logger.Log.Info().Int64("network_id", c.networkID).Msg("Disconnect callback: writing messages to channels")

	var channelsToNotify []storage.Channel
	if c.network.Nickname != "" {
		// Get channels that are open or where we're joined
		openChannels, err := c.storage.GetOpenChannels(c.networkID, c.network.Nickname)
		if err == nil && len(openChannels) > 0 {
			channelsToNotify = openChannels
			logger.Log.Debug().Int64("network_id", c.networkID).Int("open_channels", len(openChannels)).Msg("Disconnect callback: found open channels via GetOpenChannels")
		} else if err != nil {
			logger.Log.Debug().Err(err).Int64("network_id", c.networkID).Msg("Disconnect callback: error getting open channels")
		}
	}

	// Fallback: if GetOpenChannels didn't work or returned nothing, get all channels
	// and filter to those that are open
	if len(channelsToNotify) == 0 {
		allChannels, err := c.storage.GetChannels(c.networkID)
		if err == nil {
			for _, ch := range allChannels {
				if ch.IsOpen {
					channelsToNotify = append(channelsToNotify, ch)
				}
			}
			logger.Log.Debug().Int64("network_id", c.networkID).Int("open_channels", len(channelsToNotify)).Int("total_channels", len(allChannels)).Msg("Disconnect callback: found open channels via fallback")
		} else {
			logger.Log.Debug().Err(err).Int64("network_id", c.networkID).Msg("Disconnect callback: error getting all channels")
		}
	}

	// Write "Disconnected" message to each channel
	if len(channelsToNotify) > 0 {
		logger.Log.Info().Int64("network_id", c.networkID).Int("channel_count", len(channelsToNotify)).Msg("Disconnect callback: writing disconnect messages to channels")
		for _, channel := range channelsToNotify {
			disconnectMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   &channel.ID,
				User:        "*",
				Message:     "Disconnected",
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     "",
			}
			// Use WriteMessageDirect to ensure immediate persistence
			if err := c.storage.WriteMessageDirect(disconnectMsg); err != nil {
				if err.Error() != "storage is closed" {
					logger.Log.Warn().Err(err).Int64("network_id", c.networkID).Str("channel", channel.Name).Msg("Disconnect callback: failed to write disconnect message to channel")
				} else {
					logger.Log.Debug().Int64("network_id", c.networkID).Str("channel", channel.Name).Msg("Disconnect callback: storage closed, skipping channel")
				}
			} else {
				logger.Log.Debug().Int64("network_id", c.networkID).Str("channel", channel.Name).Int64("channel_id", channel.ID).Msg("Disconnect callback: wrote disconnect message to channel")
			}
		}
	}

	// Clear all channel user lists for this network since we're disconnected
	// This prevents showing stale user lists when reconnecting
	channels, err := c.storage.GetChannels(c.networkID)
	if err == nil {
		for _, ch := range channels {
			if err := c.storage.ClearChannelUsers(ch.ID); err != nil {
				logger.Log.Error().Err(err).Str("channel", ch.Name).Msg("Failed to clear users for channel")
			} else {
				logger.Log.Debug().Str("channel", ch.Name).Msg("Cleared user list for channel")
			}
		}
	}

	// Store disconnection message in status window
	rawLine, _ := e.Line()
	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil, // Status window
		User:        "*",
		Message:     "Disconnected from server",
		MessageType: "status",
		Timestamp:   time.Now(),
		RawLine:     rawLine,
	}
	c.storage.WriteMessage(statusMsg)

	c.eventBus.Emit(events.Event{
		Type:      EventConnectionLost,
		Data:      map[string]interface{}{"network": c.network.Address, "networkId": c.networkID},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})

	logger.Log.Debug().Int64("network_id", c.networkID).Msg("Disconnect callback: finished processing disconnect")
}

// setupHandlers sets up IRC event handlers
func (c *IRCClient) setupHandlers() {
	// Connection established
	c.conn.AddConnectCallback(c.onConnect)

	// Connection lost
	c.conn.AddDisconnectCallback(c.onDisconnect)

	// PRIVMSG received
	c.conn.AddCallback("PRIVMSG", c.handlePrivmsg)

	// User joined channel
	c.conn.AddCallback("JOIN", func(e ircmsg.Message) {
		channel := e.Params[0]
		user := e.Nick()

		// IRCv3 bot mode: a bot's JOIN carries the `bot` tag too, so we can
		// recognize it before it speaks.
		c.maybeMarkBotFromTag(e)
		// account-tag: a JOIN may also carry the `@account` tag.
		c.maybeApplyAccountTag(e)
		// extended-join: when negotiated, JOIN carries the joiner's account.
		c.maybeApplyExtendedJoin(e)

		logger.Log.Debug().
			Str("user", user).
			Str("channel", channel).
			Str("network", c.network.Name).
			Str("our_nick", c.network.Nickname).
			Msg("User joined channel")

		// Get or create channel in database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		channelCreated := false
		if err != nil {
			// Channel doesn't exist, create it
			logger.Log.Debug().Str("channel", channel).Msg("Channel doesn't exist, creating it")
			now := time.Now()
			newChannel := &storage.Channel{
				NetworkID: c.networkID,
				Name:      channel,
				AutoJoin:  false,
				IsOpen:    false, // Dialog not open yet - will be opened when user selects it
				CreatedAt: time.Now(),
				UpdatedAt: &now,
			}
			if err := c.storage.CreateChannel(newChannel); err != nil {
				logger.Log.Error().Err(err).Str("channel", channel).Msg("Failed to create channel")
				// Still try to continue with the rest
			} else {
				logger.Log.Debug().Str("channel", channel).Msg("Successfully created channel in database")
				ch = newChannel
				channelCreated = true
			}
		}

		// If the current user joined, mark channel as open (JOINED state)
		channelUpdated := false
		if c.isMe(user) && ch != nil {
			if !ch.IsOpen {
				c.storage.UpdateChannelIsOpen(ch.ID, true)
				channelUpdated = true
			}
		}

		// Add user to channel user list (for all users, not just ourselves)
		if ch != nil {
			if err := c.storage.AddChannelUser(ch.ID, user, ""); err != nil {
				logger.Log.Error().Err(err).Str("user", user).Str("channel", channel).Msg("Failed to add user to channel user list")
			} else {
				logger.Log.Debug().Str("user", user).Str("channel", channel).Msg("Added user to channel user list")
				// Verify the write is committed by reading it back (forces WAL sync)
				// This ensures the user is immediately visible when the frontend queries
				_, _ = c.storage.GetChannelUsers(ch.ID)
			}
		}

		// If the current user joined, the server automatically sends the NAMES
		// list (RPL_NAMREPLY 353 / RPL_ENDOFNAMES 366) as part of the JOIN
		// response; those handlers clear and repopulate channel_users and emit
		// channel.names.complete. We deliberately do NOT send an explicit NAMES
		// here — it's redundant with the server's automatic reply and doubles the
		// outgoing burst when rejoining a whole session after a reconnect.
		if c.isMe(user) {
			logger.Log.Info().
				Str("channel", channel).
				Int64("network_id", c.networkID).
				Msg("Our user joined; awaiting server NAMES reply to populate user list")

			// With no-implicit-names the server won't send the automatic NAMES
			// reply, so request it explicitly — otherwise the roster stays empty
			// (WHOX seeds attributes but not membership prefixes).
			if cmd := c.namesOnSelfJoin(channel); cmd != "" {
				if err := c.conn.SendRaw(cmd); err != nil {
					logger.Log.Debug().Err(err).Str("channel", channel).Msg("explicit NAMES request failed")
				}
			}

			// Catch-up: pull recent server-side history for the channel so anything
			// said while we were away is backfilled. No-op when the server didn't
			// grant CHATHISTORY. Replays arrive via handleChatHistoryBatch and are
			// deduped by msgid, so re-joining a channel won't duplicate messages.
			if c.chatHistoryEnabled() {
				if err := c.RequestChatHistoryLatest(channel, defaultChatHistoryLimit); err != nil {
					logger.Log.Debug().Err(err).Str("channel", channel).Msg("CHATHISTORY LATEST request skipped")
				}
			}

			// Bulk-seed the roster (account/host/away/realname) with one extended
			// WHO when the server supports WHOX, instead of waiting for live churn.
			if c.whoxSupported() {
				if err := c.requestWHOX(channel); err != nil {
					logger.Log.Debug().Err(err).Str("channel", channel).Msg("WHOX request skipped")
				}
			}
		}

		// Store join message in the channel (use sync write so it appears immediately)
		if ch != nil {
			rawLine, _ := e.Line()
			joinMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   &ch.ID,
				User:        user,
				Message:     fmt.Sprintf("%s joined the channel", user),
				MessageType: "join",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			if err := c.storage.WriteMessageSync(joinMsg); err != nil {
				// During shutdown, storage may be closed - this is expected
				if err.Error() == "storage is closed" {
					logger.Log.Debug().Msg("Skipping join message storage (storage closed during shutdown)")
				} else {
					logger.Log.Error().Err(err).Msg("Failed to store join message")
				}
			}
		}

		// Emit event
		c.eventBus.Emit(events.Event{
			Type: EventUserJoined,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"user":      user,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})

		// Emit channels changed event if channel was created or updated
		if channelCreated || (channelUpdated && c.isMe(user)) {
			c.eventBus.Emit(events.Event{
				Type: EventChannelsChanged,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// User parted channel
	c.conn.AddCallback("PART", c.handlePart)

	// ERR_LINKCHANNEL (470): a JOIN was forwarded to another channel (+f). Close
	// the requested channel's buffer so it doesn't linger as a phantom membership.
	c.conn.AddCallback("470", c.handleForwardedJoin)

	// Standard JOIN-failure numerics: close the phantom channel and explain why,
	// reusing the 470 forwarding pattern. Protocol-standard, no services logic.
	c.conn.AddCallback("471", c.handleJoinError) // ERR_CHANNELISFULL
	c.conn.AddCallback("473", c.handleJoinError) // ERR_INVITEONLYCHAN
	c.conn.AddCallback("474", c.handleJoinError) // ERR_BANNEDFROMCHAN
	c.conn.AddCallback("475", c.handleJoinError) // ERR_BADCHANNELKEY
	c.conn.AddCallback("477", c.handleJoinError) // ERR_NEEDREGGEDNICK

	// Surface common server error numerics that otherwise vanish — there is no
	// catch-all callback in ergochat/irc-go, so an unregistered numeric is dropped
	// without any user-visible output. See genericErrorNumerics.
	for _, code := range genericErrorNumerics {
		c.conn.AddCallback(code, c.handleServerNumeric)
	}

	// User quit
	c.conn.AddCallback("QUIT", c.handleQuit)

	// User kicked from channel
	c.conn.AddCallback("KICK", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[0]
		kickedUser := e.Params[1]
		kicker := e.Nick()
		reason := ""
		if len(e.Params) > 2 {
			reason = e.Params[2]
		}

		// Get channel from database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		channelUpdated := false
		if err == nil {
			// Remove kicked user from channel user list
			if err := c.storage.RemoveChannelUser(ch.ID, kickedUser); err != nil {
				logger.Log.Error().Err(err).Str("user", kickedUser).Str("channel", channel).Msg("Failed to remove kicked user from channel user list")
			} else {
				logger.Log.Debug().Str("user", kickedUser).Str("channel", channel).Msg("Removed kicked user from channel user list")
				// Verify the write is committed by reading it back (forces WAL sync)
				// This ensures the user is immediately removed when the frontend queries
				_, _ = c.storage.GetChannelUsers(ch.ID)
			}

			// If we were kicked, mark channel as closed
			if c.isMe(kickedUser) {
				c.storage.UpdateChannelIsOpen(ch.ID, false)
				channelUpdated = true
			}

			// Store kick message in the channel (use sync write so it appears immediately)
			rawLine, _ := e.Line()
			kickMsg := storage.Message{
				NetworkID: c.networkID,
				ChannelID: &ch.ID,
				User:      kicker,
				Message: fmt.Sprintf("%s kicked %s%s", kicker, kickedUser, func() string {
					if reason != "" {
						return fmt.Sprintf(" (%s)", reason)
					}
					return ""
				}()),
				MessageType: "kick",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			if err := c.storage.WriteMessageSync(kickMsg); err != nil {
				// During shutdown, storage may be closed - this is expected
				if err.Error() == "storage is closed" {
					logger.Log.Debug().Msg("Skipping kick message storage (storage closed during shutdown)")
				} else {
					logger.Log.Error().Err(err).Msg("Failed to store kick message")
				}
			}
		}

		// Emit event
		c.eventBus.Emit(events.Event{
			Type: EventUserKicked,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"kicker":    kicker,
				"user":      kickedUser,
				"reason":    reason,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})

		// Emit channels changed event if we were kicked
		if channelUpdated {
			c.eventBus.Emit(events.Event{
				Type: EventChannelsChanged,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// Nick change
	// Inbound NICK changes for everyone, with self-tracking. See handleNickMessage.
	c.conn.AddCallback("NICK", c.handleNickMessage)

	// Live-roster presence updates (IRCv3 away-notify / account-notify / chghost).
	// These only arrive when the matching cap was negotiated; each updates the
	// session-local UserMeta and emits EventUserMetaChanged when something changed.
	c.conn.AddCallback("AWAY", c.handleAway)
	c.conn.AddCallback("ACCOUNT", c.handleAccount)
	c.conn.AddCallback("CHGHOST", c.handleChghost)
	c.conn.AddCallback("SETNAME", c.handleSetname)
	c.conn.AddCallback("INVITE", c.handleInvite)
	c.conn.AddCallback("341", c.handleInviting)      // RPL_INVITING (INVITE send success)
	c.conn.AddCallback("443", c.handleUserOnChannel) // ERR_USERONCHANNEL (INVITE target already on channel)
	c.conn.AddCallback("354", c.handleWhoxReply)     // RPL_WHOSPCRPL (WHOX)
	// MONITOR presence: 730 RPL_MONONLINE, 731 RPL_MONOFFLINE.
	c.conn.AddCallback("730", func(e ircmsg.Message) { c.handleMonitorPresence(e, true) })
	c.conn.AddCallback("731", func(e ircmsg.Message) { c.handleMonitorPresence(e, false) })
	// 734 ERR_MONLISTFULL: the MONITOR list is full; the trailing nicks were not
	// added. We degrade gracefully — their presence simply shows as unknown.
	c.conn.AddCallback("734", func(e ircmsg.Message) {
		logger.Log.Warn().Interface("params", e.Params).Msg("MONITOR list full (734); some nicks are not being tracked")
	})
	// IRCv3 standard-replies: FAIL (error), WARN (warning), NOTE (status).
	c.conn.AddCallback("FAIL", func(e ircmsg.Message) { c.handleStandardReply(e, "error") })
	c.conn.AddCallback("WARN", func(e ircmsg.Message) { c.handleStandardReply(e, "warning") })
	c.conn.AddCallback("NOTE", func(e ircmsg.Message) { c.handleStandardReply(e, "status") })

	// Channel topic (RPL_TOPIC = 332) - received when topic is retrieved
	c.conn.AddCallback("332", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		channel := e.Params[1]
		topic := e.Params[2]

		// Store topic in database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err == nil {
			c.storage.UpdateChannelTopic(ch.ID, topic)
		}

		c.eventBus.Emit(events.Event{
			Type: EventChannelTopic,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"topic":     topic,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// RPL_TOPICWHOTIME (333) - who set the current topic and when. Arrives right
	// after 332 on join / topic query. Wire form: "<me> <channel> <setter> <unixtime>".
	// Without a callback irc-go drops it, so the "topic set by …" context is lost.
	c.conn.AddCallback("333", func(e ircmsg.Message) {
		if len(e.Params) < 4 {
			return
		}
		channel := e.Params[1]
		setter := e.Params[2]
		var setAt int64
		fmt.Sscanf(e.Params[3], "%d", &setAt)
		c.writeChannelSystemLine(channel, "status", formatTopicWhoTime(setter, time.Unix(setAt, 0)))
	})

	// RPL_NOTOPIC (331) - the channel has no topic set. Wire form:
	// "<me> <channel> :No topic is set". Surfaced as a channel status line.
	c.conn.AddCallback("331", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[1]
		text := "No topic is set"
		if len(e.Params) >= 3 && e.Params[len(e.Params)-1] != "" {
			text = e.Params[len(e.Params)-1]
		}
		c.writeChannelSystemLine(channel, "status", text)
	})

	// TOPIC command - when someone changes the topic
	c.conn.AddCallback("TOPIC", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[0]
		topic := ""
		if len(e.Params) > 1 {
			topic = e.Params[1]
		}

		// Store topic in database
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err == nil {
			c.storage.UpdateChannelTopic(ch.ID, topic)
		}

		c.eventBus.Emit(events.Event{
			Type: EventChannelTopic,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"topic":     topic,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Track NAMES responses for building user list
	// RPL_NAMREPLY (353) - list of users in channel
	c.conn.AddCallback("353", func(e ircmsg.Message) {
		if len(e.Params) < 4 {
			return
		}
		channel := e.Params[2]
		namesList := e.Params[3]

		// Get channel from database (case-insensitive lookup)
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err != nil {
			logger.Log.Warn().Err(err).Str("channel", channel).Int64("network_id", c.networkID).Msg("Failed to find channel for NAMES response")
			return
		}

		// Use lowercase channel name as key for namesInProgress to handle case variations
		channelKey := strings.ToLower(channel)

		// On first NAMES response for a channel, clear the existing user list
		// This ensures we start fresh when receiving the NAMES list
		c.namesMu.Lock()
		if !c.namesInProgress[channelKey] {
			c.namesInProgress[channelKey] = true
			c.namesMu.Unlock()
			if err := c.storage.ClearChannelUsers(ch.ID); err != nil {
				logger.Log.Error().Err(err).Str("channel", channel).Msg("Failed to clear users for channel")
			} else {
				logger.Log.Debug().Str("channel", channel).Int("names_count", len(strings.Fields(namesList))).Msg("Cleared user list for channel before receiving NAMES")
			}
		} else {
			c.namesMu.Unlock()
		}

		// Determine the valid membership-prefix chars from the server's ISUPPORT
		// PREFIX, falling back to the standard set if 005 hasn't been parsed yet.
		// Computed once per response rather than per-name.
		c.mu.RLock()
		validPrefixes := string(c.serverCapabilities.prefixRank())
		c.mu.RUnlock()
		if validPrefixes == "" {
			validPrefixes = standardMembershipPrefixes
		}

		// Parse names (format: "@nick1 +nick2 nick3"; with multi-prefix: "@+nick1 …")
		names := strings.Fields(namesList)
		addedCount := 0
		for _, nameWithMode := range names {
			// Extract mode prefixes (@, +, etc.) and nickname. With multi-prefix
			// enabled this captures every prefix the user holds, highest first.
			modes, rest := splitMembershipPrefixes(nameWithMode, validPrefixes)
			// With userhost-in-names the remainder is "nick!user@host"; otherwise
			// it is a bare nick. Store the bare nick and seed the roster host.
			nickname, userhost := splitNickUserHost(rest)
			if len(nickname) > 0 {
				if err := c.storage.AddChannelUser(ch.ID, nickname, modes); err != nil {
					logger.Log.Warn().Err(err).Str("nickname", nickname).Str("channel", channel).Msg("Failed to add user from NAMES")
				} else {
					addedCount++
				}
				if userhost != "" {
					c.applyUserMeta(nickname, func(m *UserMeta) { m.Host = userhost })
				}
			}
		}
		logger.Log.Debug().Str("channel", channel).Int("added", addedCount).Int("total_in_response", len(names)).Msg("Processed NAMES response")
	})

	// RPL_ENDOFNAMES (366) - end of NAMES list
	c.conn.AddCallback("366", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[1]
		channelKey := strings.ToLower(channel)
		logger.Log.Debug().Str("channel", channel).Msg("Finished receiving user list for channel")

		// Mark that we're done receiving NAMES for this channel
		c.namesMu.Lock()
		delete(c.namesInProgress, channelKey)
		c.namesMu.Unlock()

		// Get the list of users in the channel to include in the event
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		var users []string
		if err == nil {
			channelUsers, err := c.storage.GetChannelUsers(ch.ID)
			if err == nil {
				users = make([]string, len(channelUsers))
				for i, cu := range channelUsers {
					users[i] = cu.Nickname
				}
			}
		}

		// Emit event when NAMES list is complete so frontend can refresh user list
		// Include user list so plugins can process all users at once
		logger.Log.Info().
			Str("channel", channel).
			Int("user_count", len(users)).
			Int64("network_id", c.networkID).
			Msg("NAMES list complete, emitting channel.names.complete event")
		c.eventBus.Emit(events.Event{
			Type: "channel.names.complete",
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"users":     users,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Handle IRC error numerics (400-599 range)
	// Common errors: 482 ERR_CHANOPRIVSNEEDED, 442 ERR_NOTONCHANNEL, 481 ERR_NOPRIVILEGES, etc.
	c.conn.AddCallback("482", func(e ircmsg.Message) {
		// ERR_CHANOPRIVSNEEDED - You're not a channel operator
		// Format: :server 482 nick #channel :You're not a channel operator
		errorMsg := "You're not a channel operator"
		var channel string
		if len(e.Params) >= 2 {
			channel = e.Params[1] // Channel is typically the second param
			if len(e.Params) >= 3 {
				errorMsg = e.Params[2] // Error message is usually the last param
			}
		}

		rawLine, _ := e.Line()

		// Try to store error in the channel if it's a channel-related error
		var channelID *int64
		if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
			ch, err := c.storage.GetChannelByName(c.networkID, channel)
			if err == nil {
				channelID = &ch.ID
			}
		}

		// Store error in channel (if found) or status window
		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   channelID, // Channel if found, otherwise status window
			User:        "*",
			Message:     fmt.Sprintf("Error: %s", errorMsg),
			MessageType: "error",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		c.storage.WriteMessage(statusMsg)

		// Emit error event with channel info
		c.eventBus.Emit(events.Event{
			Type: EventError,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"error":     errorMsg,
				"code":      "482",
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// 442 ERR_NOTONCHANNEL
	c.conn.AddCallback("442", func(e ircmsg.Message) {
		// Format: :server 442 nick #channel :You're not on that channel
		errorMsg := "You're not on that channel"
		var channel string
		if len(e.Params) >= 2 {
			channel = e.Params[1]
			if len(e.Params) >= 3 {
				errorMsg = e.Params[2]
			}
		}

		rawLine, _ := e.Line()

		// Try to store error in the channel if it's a channel-related error
		var channelID *int64
		if len(channel) > 0 && (channel[0] == '#' || channel[0] == '&') {
			ch, err := c.storage.GetChannelByName(c.networkID, channel)
			if err == nil {
				channelID = &ch.ID
			}
		}

		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   channelID,
			User:        "*",
			Message:     fmt.Sprintf("Error: %s", errorMsg),
			MessageType: "error",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		c.storage.WriteMessage(statusMsg)

		c.eventBus.Emit(events.Event{
			Type: EventError,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"error":     errorMsg,
				"code":      "442",
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// 481 ERR_NOPRIVILEGES
	c.conn.AddCallback("481", func(e ircmsg.Message) {
		// Format: :server 481 nick :Permission denied
		errorMsg := "Permission denied"
		if len(e.Params) >= 2 {
			errorMsg = e.Params[len(e.Params)-1]
		}

		rawLine, _ := e.Line()
		statusMsg := storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil, // This error is usually not channel-specific
			User:        "*",
			Message:     fmt.Sprintf("Error: %s", errorMsg),
			MessageType: "error",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		}
		c.storage.WriteMessage(statusMsg)

		c.eventBus.Emit(events.Event{
			Type: EventError,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"error":     errorMsg,
				"code":      "481",
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// Channel MODE changes
	c.conn.AddCallback("MODE", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		target := e.Params[0]
		// Channel modes start with # or &. A non-channel target is a user mode;
		// we only care about our own +<botletter> echo (so our own nick carries the
		// bot badge consistently), then return — other user modes are still ignored.
		if len(target) == 0 || (target[0] != '#' && target[0] != '&') {
			c.markSelfBotFromUserMode(target, e.Params[1])
			return
		}

		ch, err := c.storage.GetChannelByName(c.networkID, target)
		if err != nil {
			return
		}

		// Snapshot the server's mode grammar once under lock, then parse lock-free.
		c.mu.RLock()
		cls := c.serverCapabilities.classification()
		c.mu.RUnlock()

		changes := ParseModeChanges(e.Params[1], e.Params[2:], cls)

		// Surface the change in the channel itself (like join/part/kick), faithfully
		// mirroring what the server applied: "<actor> sets mode: +o-v+k a b key".
		// Bare list-mode *queries* (e.g. ban-list fetches) never reach here — the
		// server answers those with 367/368, not a MODE echo — so this won't fire for them.
		if len(changes) > 0 {
			actor := e.Nick()
			if actor == "" {
				actor = e.Source
			}
			rawLine, _ := e.Line()
			c.storage.WriteMessageSync(storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   &ch.ID,
				User:        actor,
				Message:     fmt.Sprintf("%s sets mode: %s", actor, strings.Join(e.Params[1:], " ")),
				MessageType: "mode",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			})
		}

		// Fold channel-level changes (D flags + B/C params) into the canonical mode
		// string, persisting only when it actually changed.
		newModes := applyChannelModes(ch.Modes, changes, cls)
		if newModes != ch.Modes {
			if err := c.storage.UpdateChannelModes(ch.ID, newModes); err != nil {
				logger.Log.Warn().Err(err).Str("channel", target).Msg("Failed to persist channel modes")
			}
		}
		// Always announce the mode change so the UI reloads the channel — both the modes
		// header and the "sets mode" line written above. This fires even for list-mode
		// changes (e.g. +b) where the canonical string is unchanged, which would otherwise
		// leave the new line invisible until the next poll.
		c.eventBus.Emit(events.Event{
			Type: EventChannelMode,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   target,
				"modes":     newModes,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})

		// Apply per-user membership (prefix) changes individually, emitting a granular
		// event per affected user so the member list re-groups live.
		for _, mc := range changes {
			if mc.Kind != ModeKindPrefix || mc.Param == "" {
				continue
			}
			current, gerr := c.storage.GetChannelUserModes(ch.ID, mc.Param)
			if gerr != nil {
				// User not tracked yet (e.g. NAMES not complete) — skip.
				continue
			}
			c.mu.RLock()
			updated := c.serverCapabilities.applyUserPrefix(current, mc.Mode, mc.Add)
			c.mu.RUnlock()
			if updated == current {
				continue
			}
			if err := c.storage.AddChannelUser(ch.ID, mc.Param, updated); err != nil {
				logger.Log.Warn().Err(err).Str("nick", mc.Param).Str("channel", target).Msg("Failed to update user modes")
				continue
			}
			added, removed := "", ""
			if mc.Add {
				added = string(mc.Mode)
			} else {
				removed = string(mc.Mode)
			}
			c.eventBus.Emit(events.Event{
				Type: EventChannelUserMode,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
					"channel":   target,
					"nick":      mc.Param,
					"modes":     updated,
					"added":     added,
					"removed":   removed,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// RPL_CHANNELMODEIS (324) - authoritative channel modes, typically on join or
	// in response to a "MODE #channel" query. Replaces the stored canonical string.
	c.conn.AddCallback("324", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		channel := e.Params[1]
		ch, err := c.storage.GetChannelByName(c.networkID, channel)
		if err != nil {
			return
		}
		c.mu.RLock()
		cls := c.serverCapabilities.classification()
		c.mu.RUnlock()

		changes := ParseModeChanges(e.Params[2], e.Params[3:], cls)
		newModes := applyChannelModes("", changes, cls) // authoritative: rebuild from scratch
		if err := c.storage.UpdateChannelModes(ch.ID, newModes); err != nil {
			logger.Log.Warn().Err(err).Str("channel", channel).Msg("Failed to persist channel modes from 324")
			return
		}
		c.eventBus.Emit(events.Event{
			Type: EventChannelMode,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"modes":     newModes,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// RPL_BANLIST (367) - a single ban entry: <channel> <mask> [<setter> <time>]
	c.conn.AddCallback("367", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		channel := e.Params[1]
		entry := BanEntry{Mask: e.Params[2]}
		if len(e.Params) >= 4 {
			entry.By = e.Params[3]
		}
		if len(e.Params) >= 5 {
			fmt.Sscanf(e.Params[4], "%d", &entry.Time)
		}
		key := strings.ToLower(channel)
		c.banListsMu.Lock()
		c.banLists[key] = append(c.banLists[key], entry)
		c.banListsMu.Unlock()
	})

	// RPL_ENDOFBANLIST (368) - end of ban list; flush collected entries to the frontend.
	c.conn.AddCallback("368", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		channel := e.Params[1]
		key := strings.ToLower(channel)
		c.banListsMu.Lock()
		entries := c.banLists[key]
		delete(c.banLists, key)
		c.banListsMu.Unlock()

		bans := make([]interface{}, len(entries))
		for i, b := range entries {
			bans[i] = map[string]interface{}{"mask": b.Mask, "by": b.By, "time": b.Time}
		}
		c.eventBus.Emit(events.Event{
			Type: EventChannelBanList,
			Data: map[string]interface{}{
				"network":   c.network.Address,
				"networkId": c.networkID,
				"channel":   channel,
				"bans":      bans,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// MOTD (Message of the Day) - store in status window
	c.conn.AddCallback("372", func(e ircmsg.Message) {
		// RPL_MOTD line
		if len(e.Params) >= 2 {
			motdLine := e.Params[1]
			rawLine, _ := e.Line()
			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil, // Status window
				User:        "*",
				Message:     motdLine,
				MessageType: "motd",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			c.storage.WriteMessage(statusMsg)
		}
	})

	// NOTICE messages - store in status window if not from a channel
	// Also handle CTCP responses (CTCP replies come as NOTICE)
	c.conn.AddCallback("NOTICE", c.handleNotice)

	// TAGMSG carries IRCv3 client-only tags with no message body; handleTypingTag
	// surfaces the +typing client tag (typing indicators) and ignores everything
	// else. Ephemeral — nothing is stored.
	c.conn.AddCallback("TAGMSG", c.handleTypingTag)

	// CHATHISTORY replays arrive wrapped in a BATCH. The library buffers the whole
	// group and hands it to batch callbacks; handleChatHistoryBatch claims the
	// "chathistory" batches (bulk dedup-insert + a single history event) and lets
	// every other batch type fall through to the library's default dispatch.
	c.conn.AddBatchCallback(c.handleChatHistoryBatch)

	// Numeric replies (like RPL_WELCOME, etc.) - store important ones in status
	c.conn.AddCallback("001", func(e ircmsg.Message) {
		// RPL_WELCOME
		if len(e.Params) >= 1 {
			welcomeMsg := e.Params[len(e.Params)-1]
			rawLine, _ := e.Line()
			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil, // Status window
				User:        "*",
				Message:     welcomeMsg,
				MessageType: "status",
				Timestamp:   time.Now(),
				RawLine:     rawLine,
			}
			c.storage.WriteMessage(statusMsg)
		}
	})

	// Track the nick the server actually assigned us (an alternative if our
	// preferred nick was taken). Registered after the welcome-line writer above
	// so the log shows the welcome first; see handleWelcome.
	c.conn.AddCallback("001", c.handleWelcome)

	// Auto-join is gated on registration completion. The end-of-MOTD numerics are
	// the primary trigger because they guarantee ISUPPORT (005) has arrived, so
	// NAMES prefix parsing is correct before the first reply. A fallback timer
	// armed at RPL_WELCOME (001) covers servers that send no MOTD terminator.
	// autoJoinOnce ensures whichever fires first wins and we join exactly once.
	c.conn.AddCallback("001", func(e ircmsg.Message) {
		c.mu.RLock()
		once := c.autoJoinOnce
		c.mu.RUnlock()
		time.AfterFunc(constants.AutoJoinDelay, func() {
			// Re-read so a Connect() that re-armed the Once in the meantime isn't
			// triggered by this stale timer.
			c.mu.RLock()
			current := c.autoJoinOnce
			c.mu.RUnlock()
			if current == once {
				c.triggerAutoJoin()
			}
		})
	})
	c.conn.AddCallback("376", func(e ircmsg.Message) { c.triggerAutoJoin() }) // RPL_ENDOFMOTD
	c.conn.AddCallback("422", func(e ircmsg.Message) { c.triggerAutoJoin() }) // ERR_NOMOTD

	// WHOIS response handlers
	// RPL_WHOISUSER (311) - Basic user information
	// RPL_AWAY (301) - "<me> <nick> :<away message>". Sent during a WHOIS for an
	// away user, and unsolicited when we message someone who is away. Without a
	// callback irc-go drops it, so the away message never surfaced. When a WHOIS
	// is in progress we fold it into that result; otherwise we show a status line.
	c.conn.AddCallback("301", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nick := e.Params[1]
		away := e.Params[len(e.Params)-1]
		c.whoisMu.Lock()
		wi := c.whoisInProgress[nick]
		if wi != nil {
			wi.Away = away
		}
		c.whoisMu.Unlock()
		if wi == nil {
			c.writeStatusLine("status", fmt.Sprintf("%s is away: %s", nick, away))
		}
	})

	c.conn.AddCallback("311", func(e ircmsg.Message) {
		if len(e.Params) < 4 {
			return
		}
		nickname := e.Params[1]
		username := e.Params[2]
		hostmask := e.Params[3]
		realName := ""
		if len(e.Params) >= 5 {
			realName = e.Params[4]
		}

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].Username = username
		c.whoisInProgress[nickname].Hostmask = hostmask
		c.whoisInProgress[nickname].RealName = realName
		c.whoisMu.Unlock()
	})

	// RPL_WHOISSERVER (312) - Server information
	c.conn.AddCallback("312", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nickname := e.Params[1]
		server := e.Params[2]
		serverInfo := ""
		if len(e.Params) >= 4 {
			serverInfo = e.Params[3]
		}

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].Server = server
		c.whoisInProgress[nickname].ServerInfo = serverInfo
		c.whoisMu.Unlock()
	})

	// RPL_WHOISOPERATOR (313) - Operator status
	c.conn.AddCallback("313", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		nickname := e.Params[1]
		// User is an IRC operator - we can store this in a future field if needed
		logger.Log.Debug().Str("nickname", nickname).Msg("User is an IRC operator")
	})

	// RPL_WHOISIDLE (317) - Idle time and sign-on time
	c.conn.AddCallback("317", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nickname := e.Params[1]
		idleSeconds := int64(0)
		signOnTime := int64(0)

		// Parse idle time (seconds)
		if len(e.Params) >= 3 {
			fmt.Sscanf(e.Params[2], "%d", &idleSeconds)
		}
		// Parse sign-on time (unix timestamp)
		if len(e.Params) >= 4 {
			fmt.Sscanf(e.Params[3], "%d", &signOnTime)
		}

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].IdleTime = idleSeconds
		c.whoisInProgress[nickname].SignOnTime = signOnTime
		c.whoisMu.Unlock()
	})

	// RPL_ENDOFWHOIS (318) - End of WHOIS
	c.conn.AddCallback("318", func(e ircmsg.Message) {
		if len(e.Params) < 2 {
			return
		}
		nickname := e.Params[1]

		c.whoisMu.Lock()
		whoisInfo := c.whoisInProgress[nickname]
		delete(c.whoisInProgress, nickname)
		c.whoisMu.Unlock()

		if whoisInfo != nil {
			// Emit WHOIS event with complete information
			c.eventBus.Emit(events.Event{
				Type: EventWhoisReceived,
				Data: map[string]interface{}{
					"network":   c.network.Address,
					"networkId": c.networkID,
					"whois":     whoisInfo,
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	})

	// RPL_WHOISCHANNELS (319) - Channels user is in
	c.conn.AddCallback("319", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nickname := e.Params[1]
		channelsStr := e.Params[2]

		// Parse channels (space-separated list)
		channels := strings.Fields(channelsStr)

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].Channels = channels
		c.whoisMu.Unlock()
	})

	// RPL_WHOISACCOUNT (330) - Account name (if logged in)
	c.conn.AddCallback("330", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		nickname := e.Params[1]
		accountName := e.Params[2]

		c.whoisMu.Lock()
		if c.whoisInProgress[nickname] == nil {
			c.whoisInProgress[nickname] = &WhoisInfo{
				Nickname: nickname,
				Network:  c.network.Address,
			}
		}
		c.whoisInProgress[nickname].AccountName = accountName
		c.whoisMu.Unlock()
	})

	// RPL_WHOISBOT (335) - Target is a bot (IRCv3 bot mode)
	c.conn.AddCallback("335", c.handleWhoisBot)

	// RPL_MYINFO (004) - Server software identity.
	c.conn.AddCallback("004", func(e ircmsg.Message) { c.applyMyInfo(e) })

	// ISUPPORT (005) - Server capabilities
	c.conn.AddCallback("005", func(e ircmsg.Message) {
		// RPL_ISUPPORT - Server capability parameters
		// Format: :server 005 nickname PREFIX=(ov)@+ CHANMODES=b,k,l,imnpst ... :are supported by this server
		if len(e.Params) < 2 {
			return
		}

		// Parse all parameters (skip first param which is our nickname)
		for i := 1; i < len(e.Params); i++ {
			param := e.Params[i]
			if param == "" || param[0] == ':' {
				// Last parameter starts with ':' and is usually a description
				break
			}
			c.applyISUPPORTToken(param)
		}
	})

	// RPL_LISTSTART (321) - Beginning of LIST response
	c.conn.AddCallback("321", func(e ircmsg.Message) {
		c.channelListMu.Lock()
		c.channelListItems = nil // Reset list
		c.channelListMu.Unlock()
		logger.Log.Debug().Int64("network_id", c.networkID).Msg("Channel LIST started")
	})

	// RPL_LIST (322) - Channel list entry: <channel> <visible> :<topic>
	c.conn.AddCallback("322", func(e ircmsg.Message) {
		if len(e.Params) < 3 {
			return
		}
		channelName := e.Params[1]
		usersCount := 0
		fmt.Sscanf(e.Params[2], "%d", &usersCount)
		topic := ""
		if len(e.Params) >= 4 {
			topic = e.Params[3]
		}

		item := ChannelListItem{
			Channel:   channelName,
			Users:     usersCount,
			Topic:     topic,
			NetworkID: c.networkID,
		}

		c.channelListMu.Lock()
		c.channelListItems = append(c.channelListItems, item)
		c.channelListMu.Unlock()
	})

	// RPL_LISTEND (323) - End of LIST response
	c.conn.AddCallback("323", func(e ircmsg.Message) {
		c.channelListMu.Lock()
		items := make([]ChannelListItem, len(c.channelListItems))
		copy(items, c.channelListItems)
		c.channelListItems = nil
		c.channelListMu.Unlock()

		logger.Log.Debug().Int64("network_id", c.networkID).Int("count", len(items)).Msg("Channel LIST completed")

		// Convert items to interface slice for event data
		itemsData := make([]interface{}, len(items))
		for i, item := range items {
			itemsData[i] = map[string]interface{}{
				"channel":   item.Channel,
				"users":     item.Users,
				"topic":     item.Topic,
				"networkId": item.NetworkID,
			}
		}

		c.eventBus.Emit(events.Event{
			Type: EventChannelListEnd,
			Data: map[string]interface{}{
				"networkId": c.networkID,
				"channels":  itemsData,
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
	})

	// IRCv3 capability negotiation (always enabled, not just for SASL)
	var capLSBuffer strings.Builder

	c.conn.AddCallback("CAP", func(e ircmsg.Message) {
		rawLine, _ := e.Line()
		c.storage.WriteMessage(storage.Message{
			NetworkID:   c.networkID,
			ChannelID:   nil,
			User:        "*",
			Message:     fmt.Sprintf("CAP received: %s", rawLine),
			MessageType: "status",
			Timestamp:   time.Now(),
			RawLine:     rawLine,
		})

		if len(e.Params) < 2 {
			return
		}
		subcommand := e.Params[1]

		switch subcommand {
		case "LS":
			// CAP LS 302 may split the advertisement across multiple lines; only
			// the final line (no "*" continuation marker) completes the list.
			allCaps, complete := accumulateCapLS(e.Params, &capLSBuffer)
			if complete {
				capLSBuffer.Reset()

				// Record the advertised CHATHISTORY batch limit (chathistory=N /
				// draft/chathistory=N) so requests can be clamped to it. Prefer the
				// ratified name's value if both are present.
				if v, ok := capValue(allCaps, "chathistory"); ok {
					c.setChatHistoryMax(v)
				} else if v, ok := capValue(allCaps, "draft/chathistory"); ok {
					c.setChatHistoryMax(v)
				}

				// STS is informational metadata advertised in CAP LS — it is read
				// here and acted on by the App, never CAP REQ'd (so it is absent
				// from requestedCaps above).
				if v, ok := capValue(allCaps, "sts"); ok {
					c.handleSTSAdvertisement(v)
				}

				c.storage.WriteMessage(storage.Message{
					NetworkID:   c.networkID,
					ChannelID:   nil,
					User:        "*",
					Message:     fmt.Sprintf("CAP LS response: %s", allCaps),
					MessageType: "status",
					Timestamp:   time.Now(),
				})

				// A CAP LS can arrive twice: the initial pre-registration handshake
				// and a redundant re-advertisement after registration (the connect
				// callback re-sends CAP LS 302 on RPL_ENDOFMOTD). On the re-LS,
				// negotiation is already done — treat it like CAP NEW: request only
				// genuinely-new wanted caps, exclude SASL, and never re-send CAP END.
				c.mu.RLock()
				done := c.capNegotiationDone
				toRequest := capsToRequest(allCaps, c.enabledCaps, c.saslEnabled && !done)
				c.mu.RUnlock()

				// Required SASL not on offer: abort rather than register unauthenticated.
				if c.saslEnabled && !done && !contains(allCaps, "sasl") {
					c.handleCapLSMissingSASL(allCaps)
					return
				}

				if len(toRequest) > 0 {
					reqStr := strings.Join(toRequest, " ")
					c.storage.WriteMessage(storage.Message{
						NetworkID:   c.networkID,
						ChannelID:   nil,
						User:        "*",
						Message:     fmt.Sprintf("Requesting capabilities: %s", reqStr),
						MessageType: "status",
						Timestamp:   time.Now(),
					})
					c.conn.SendRaw("CAP REQ :" + reqStr)
					if !done && c.saslEnabled && contains(allCaps, "sasl") {
						c.saslCapRequested = true
					}
				} else if !done {
					// Only the initial handshake ends with CAP END when we want
					// nothing on offer; a post-registration re-LS just no-ops.
					c.storage.WriteMessage(storage.Message{
						NetworkID:   c.networkID,
						ChannelID:   nil,
						User:        "*",
						Message:     "No requested capabilities supported, ending CAP negotiation",
						MessageType: "status",
						Timestamp:   time.Now(),
					})
					c.endCapNegotiation()
				}
			}
		case "ACK":
			if len(e.Params) >= 3 {
				acked := e.Params[2]
				c.mu.Lock()
				for _, ackedCap := range strings.Fields(acked) {
					c.enabledCaps[ackedCap] = true
				}
				c.mu.Unlock()

				c.storage.WriteMessage(storage.Message{
					NetworkID:   c.networkID,
					ChannelID:   nil,
					User:        "*",
					Message:     fmt.Sprintf("Capabilities enabled: %s", acked),
					MessageType: "status",
					Timestamp:   time.Now(),
				})

				// Only the initial handshake ends with CAP END / starts SASL. A
				// post-registration ACK (the server's reply to a CAP NEW request)
				// just records the caps above — negotiation is already complete.
				c.mu.RLock()
				negotiationDone := c.capNegotiationDone
				c.mu.RUnlock()
				if !negotiationDone {
					if contains(acked, "sasl") && c.saslEnabled {
						c.saslCapAcknowledged = true
						c.startSASLAuth()
					} else {
						c.endCapNegotiation()
					}
				}
			}
		case "NAK":
			// A NAK during the initial handshake ends negotiation; a NAK for a
			// post-registration CAP NEW request just means we don't get that cap.
			c.mu.RLock()
			negotiationDone := c.capNegotiationDone
			saslRequired := c.saslEnabled
			c.mu.RUnlock()
			rejected := ""
			if len(e.Params) >= 3 {
				rejected = e.Params[2]
			}
			msg := fmt.Sprintf("Capability request rejected (NAK): %s", rejected)
			if !negotiationDone {
				msg = "Capability request rejected (NAK), ending CAP negotiation"
			}
			c.storage.WriteMessage(storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil,
				User:        "*",
				Message:     msg,
				MessageType: "status",
				Timestamp:   time.Now(),
			})
			if !negotiationDone {
				// If the server refused the sasl cap and SASL is required, abort
				// rather than register unauthenticated.
				if saslRequired && contains(rejected, "sasl") {
					c.handleSASLFailure("server refused SASL")
				} else {
					c.endCapNegotiation()
				}
			}
		case "NEW":
			if len(e.Params) >= 3 {
				offered := e.Params[2]

				// Record an updated CHATHISTORY batch limit if the server now
				// advertises one (mirrors the CAP LS handling above).
				if v, ok := capValue(offered, "chathistory"); ok {
					c.setChatHistoryMax(v)
				} else if v, ok := capValue(offered, "draft/chathistory"); ok {
					c.setChatHistoryMax(v)
				}

				c.storage.WriteMessage(storage.Message{
					NetworkID:   c.networkID,
					ChannelID:   nil,
					User:        "*",
					Message:     fmt.Sprintf("New capabilities available: %s", offered),
					MessageType: "status",
					Timestamp:   time.Now(),
				})

				// Request any newly-offered caps we want but don't yet have. SASL
				// is excluded: mid-session (re)authentication is out of scope.
				c.mu.RLock()
				toRequest := capsToRequest(offered, c.enabledCaps, false)
				c.mu.RUnlock()
				if len(toRequest) > 0 {
					reqStr := strings.Join(toRequest, " ")
					c.storage.WriteMessage(storage.Message{
						NetworkID:   c.networkID,
						ChannelID:   nil,
						User:        "*",
						Message:     fmt.Sprintf("Requesting capabilities: %s", reqStr),
						MessageType: "status",
						Timestamp:   time.Now(),
					})
					// No CAP END here: negotiation already ended. The server's
					// CAP ACK/NAK is handled by the cases above.
					c.conn.SendRaw("CAP REQ :" + reqStr)
				}
			}
		case "DEL":
			if len(e.Params) >= 3 {
				removed := strings.Fields(e.Params[2])
				c.mu.Lock()
				for _, capName := range removed {
					// CAP DEL lists bare names, but strip any value defensively.
					if idx := strings.Index(capName, "="); idx != -1 {
						capName = capName[:idx]
					}
					delete(c.enabledCaps, capName)
				}
				c.mu.Unlock()

				c.storage.WriteMessage(storage.Message{
					NetworkID:   c.networkID,
					ChannelID:   nil,
					User:        "*",
					Message:     fmt.Sprintf("Capabilities removed: %s", e.Params[2]),
					MessageType: "status",
					Timestamp:   time.Now(),
				})
			}
		}
	})

	// SASL-specific handlers (always registered; no-op if SASL isn't in use)
	c.conn.AddCallback("AUTHENTICATE", func(e ircmsg.Message) {
		if !c.saslEnabled {
			return
		}
		c.handleAUTHENTICATE(e)
	})

	c.conn.AddCallback("900", func(e ircmsg.Message) { // RPL_LOGGEDIN
		if !c.saslEnabled {
			return
		}
		c.handleSASLSuccess()
	})
	c.conn.AddCallback("903", func(e ircmsg.Message) { // RPL_SASLSUCCESS
		if !c.saslEnabled {
			return
		}
		c.handleSASLSuccess()
	})

	c.conn.AddCallback("901", func(e ircmsg.Message) { c.handleLoggedOut() }) // RPL_LOGGEDOUT
	c.conn.AddCallback("902", func(e ircmsg.Message) {                        // ERR_NICKLOCKED
		if !c.saslEnabled {
			return
		}
		c.handleSASLFailure("account locked")
	})
	c.conn.AddCallback("904", func(e ircmsg.Message) { // ERR_SASLFAIL
		if !c.saslEnabled {
			return
		}
		c.handleSASLFailure("invalid credentials")
	})
	c.conn.AddCallback("905", func(e ircmsg.Message) { // ERR_SASLTOOLONG
		if !c.saslEnabled {
			return
		}
		c.handleSASLFailure("credentials too long")
	})
	c.conn.AddCallback("906", func(e ircmsg.Message) { // ERR_SASLABORTED
		if !c.saslEnabled {
			return
		}
		c.handleSASLFailure("authentication aborted")
	})

	// ERR_NICKNAMEINUSE (433) - nick collision handling
	// ERR_NICKNAMEINUSE (433). The library already selects an alternative
	// pre-registration and re-requests the preferred nick on each keepalive, so
	// we don't send our own NICK here — we only surface a one-time notice.
	c.conn.AddCallback("433", c.handleNickInUse)

	// Other nick-error numerics. These carry no background-reclaim traffic, so
	// they only ever speak up when answering a user-initiated /nick (see
	// surfaceManualNickError).
	c.conn.AddCallback("431", c.handleNoNickGiven)     // ERR_NONICKNAMEGIVEN
	c.conn.AddCallback("432", c.handleErroneousNick)   // ERR_ERRONEUSNICKNAME
	c.conn.AddCallback("436", c.handleNickCollision)   // ERR_NICKCOLLISION
	c.conn.AddCallback("437", c.handleNickUnavailable) // ERR_UNAVAILRESOURCE
}

// contains checks if a capability string contains the given capability
// getMessageTime returns the server-time from the message's IRCv3 tags if available,
// otherwise returns time.Now(). Requires the server-time capability to be enabled.
func (c *IRCClient) getMessageTime(e ircmsg.Message) time.Time {
	c.mu.RLock()
	hasServerTime := c.enabledCaps["server-time"]
	c.mu.RUnlock()

	if hasServerTime {
		if present, timeTag := e.GetTag("time"); present && timeTag != "" {
			if t, err := time.Parse(time.RFC3339Nano, timeTag); err == nil {
				return t
			}
			// Also try without fractional seconds
			if t, err := time.Parse("2006-01-02T15:04:05Z", timeTag); err == nil {
				return t
			}
		}
	}
	return time.Now()
}

// isEchoMessage returns true if the message is an echo of our own message
// (sent back by the server because the echo-message capability is enabled).
func (c *IRCClient) isEchoMessage(e ircmsg.Message) bool {
	c.mu.RLock()
	hasEcho := c.enabledCaps["echo-message"]
	nick := c.currentNick
	c.mu.RUnlock()

	if !hasEcho {
		return false
	}
	if nick == "" {
		nick = c.network.Nickname // before registration assigns a live nick
	}
	return c.sameName(e.Nick(), nick)
}

// isMe reports whether the given nick is the one the server currently knows us
// by. Self-identification (JOIN/PART/KICK/PRIVMSG/NOTICE "is this me?" checks)
// must use the LIVE nick (CurrentNick), not the preferred nick: while we are on
// a fallback nick after a collision (e.g. "matt0x6f_0" because "matt0x6f" was
// taken), comparing against the preferred nick silently fails for every echo and
// self-membership event, drifting the UI away from the real socket state.
func (c *IRCClient) isMe(nick string) bool {
	return c.sameName(nick, c.CurrentNick())
}

// pmPeer returns the conversation peer (the other party) for a private message.
// For messages we sent (sender == our nick, e.g. echoed back via echo-message)
// the peer is the recipient (target); for received messages it is the sender.
// Uses the live-nick identity check (isMe) so echoes still key to the recipient
// while we are on a fallback nick after a collision.
func (c *IRCClient) pmPeer(sender, target string) string {
	if c.isMe(sender) {
		return target
	}
	return sender
}

// noticePMTarget decides whether an inbound NOTICE should be routed to a
// per-sender query pane and, if so, returns the conversation peer. Services such
// as ChanServ/NickServ (and ordinary users) reply via NOTICE, so those belong in
// the sender's query pane rather than the Status log.
//
// The source is treated as a user/service when it is a nick rather than a
// server. Two shapes occur in the wild:
//   - a full "nick!user@host" hostmask (always a client/service), or
//   - a bare nick with no '!' and no '.' (e.g. ":ChanServ NOTICE ..." — some
//     networks/services omit the user@host on service notices).
//
// A server source is a bare host name containing a '.' (e.g. "irc.libera.chat")
// or is empty; those, a "*" target (pre-registration / broadcast), and
// channel-targeted notices all stay out of any PM pane (returns ""). The peer is
// computed with the same echo-aware rule as pmPeer so an echoed self-notice keys
// to the recipient.
func (c *IRCClient) noticePMTarget(source, sender, target string) string {
	if !c.sourceIsUser(source) {
		return "" // server prefix (host name or empty) -> Status
	}
	if target == "" || target == "*" {
		return "" // broadcast / pre-registration notice -> Status
	}
	if target[0] == '#' || target[0] == '&' {
		return "" // channel notice -> not a PM
	}
	return c.pmPeer(sender, target)
}

// sourceIsUser reports whether an IRC message prefix names a user/service (a
// nick) rather than a server. A hostmask ("nick!user@host") is always a user; a
// bare token is a user only if it has no '.' (server names are dotted host
// names). An empty prefix is the server.
func (c *IRCClient) sourceIsUser(source string) bool {
	if source == "" {
		return false
	}
	if strings.Contains(source, "!") {
		return true // nick!user@host
	}
	return !strings.Contains(source, ".") // bare nick (no dot) vs dotted server name
}

// ---- IRCv3 CHATHISTORY ----

// defaultChatHistoryLimit is the per-request message count used when the server
// doesn't advertise a chathistory=N maximum.
const defaultChatHistoryLimit = 100

// setChatHistoryMax records the advertised chathistory cap value (e.g. "50").
// A missing/zero/invalid value leaves the limit unset (0 = unknown, use default).
func (c *IRCClient) setChatHistoryMax(value string) {
	n, err := strconv.Atoi(strings.TrimSpace(value))
	if err != nil || n <= 0 {
		return
	}
	c.mu.Lock()
	c.chatHistoryMaxBatch = n
	c.mu.Unlock()
}

// chatHistoryEnabled reports whether the server granted a CHATHISTORY capability
// (ratified or draft name).
func (c *IRCClient) chatHistoryEnabled() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabledCaps["chathistory"] || c.enabledCaps["draft/chathistory"]
}

// capEnabled reports whether the server granted a named IRCv3 capability.
// namesOnSelfJoin returns the explicit NAMES command to issue after our own JOIN,
// or "" when none is needed. The roster is built solely from the post-JOIN NAMES
// reply (353/366); normally the server sends it automatically. With the ratified
// no-implicit-names cap that automatic reply is suppressed, so we must request it
// ourselves or the channel's nick list would be empty.
func (c *IRCClient) namesOnSelfJoin(channel string) string {
	if channel == "" || !c.capEnabled("no-implicit-names") {
		return ""
	}
	return "NAMES " + channel
}

func (c *IRCClient) capEnabled(name string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.enabledCaps[name]
}

// clampChatHistoryLimit bounds a requested count to the default (when unset) and
// to the server-advertised maximum (when known).
func (c *IRCClient) clampChatHistoryLimit(limit int) int {
	if limit <= 0 {
		limit = defaultChatHistoryLimit
	}
	c.mu.RLock()
	max := c.chatHistoryMaxBatch
	c.mu.RUnlock()
	if max > 0 && limit > max {
		limit = max
	}
	return limit
}

// getMsgID returns the IRCv3 @msgid tag, or "" if absent.
func (c *IRCClient) getMsgID(e ircmsg.Message) string {
	if present, v := e.GetTag("msgid"); present {
		return v
	}
	return ""
}

// getHistoryTime extracts the @time server-time tag from a replayed message,
// falling back to now. Unlike getMessageTime it doesn't gate on the server-time
// cap: CHATHISTORY items always carry @time and we always want the original time.
func (c *IRCClient) getHistoryTime(e ircmsg.Message) time.Time {
	if present, timeTag := e.GetTag("time"); present && timeTag != "" {
		if t, err := time.Parse(time.RFC3339Nano, timeTag); err == nil {
			return t
		}
		if t, err := time.Parse("2006-01-02T15:04:05Z", timeTag); err == nil {
			return t
		}
	}
	return time.Now()
}

// RequestChatHistoryLatest asks the server for the most recent `limit` messages
// for target (a channel name or nick). Used for on-join / on-open catch-up.
func (c *IRCClient) RequestChatHistoryLatest(target string, limit int) error {
	if !c.chatHistoryEnabled() {
		return fmt.Errorf("server does not support chathistory")
	}
	if target == "" {
		return fmt.Errorf("chathistory target required")
	}
	limit = c.clampChatHistoryLimit(limit)
	return c.conn.SendRaw(fmt.Sprintf("CHATHISTORY LATEST %s * %d", target, limit))
}

// whoxRosterToken tags the WHO query Cascade issues on join so its 354 replies
// can be distinguished from a user-initiated WHO. WHOX echoes the token back as
// the first field of every reply row.
const whoxRosterToken = "332"

// whoxSupported reports whether the server advertised the WHOX token in ISUPPORT.
func (c *IRCClient) whoxSupported() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsWHOX
}

// requestWHOX issues an extended WHO for a channel to bulk-seed the live roster
// (account / host / away / realname) on join, instead of waiting for live churn
// to fill it in. The field set "%tcuhnfar" yields, in WHOX's canonical order:
// token, channel, user, host, nick, flags, account, realname.
//
// When labeled-response is negotiated, the request is sent via the library's
// SendWithLabel: the server's reply is collected into a batch correlated by an
// IRCv3 @label and delivered to handleWhoxBatch — no token matching needed. The
// token is still included so the reply layout is identical to the fallback path.
// Without labeled-response, a plain WHO is sent and the 354 rows are correlated
// by whoxRosterToken in handleWhoxReply.
func (c *IRCClient) requestWHOX(channel string) error {
	if channel == "" {
		return fmt.Errorf("whox channel required")
	}
	fields := fmt.Sprintf("%%tcuhnfar,%s", whoxRosterToken)
	if c.capEnabled("labeled-response") {
		if err := c.conn.SendWithLabel(c.handleWhoxBatch, nil, "WHO", channel, fields); err == nil {
			return nil
		}
		// Fall through to the unlabeled path if the label send was refused.
	}
	return c.conn.SendRaw(fmt.Sprintf("WHO %s %s", channel, fields))
}

// handleWhoxBatch receives the labeled-response batch for a roster-seeding WHO
// (see requestWHOX) and folds each 354 row into the roster. b is nil if the
// server failed to send a proper labeled response.
func (c *IRCClient) handleWhoxBatch(b *ircevent.Batch) {
	if b == nil {
		return
	}
	// A multi-row reply nests the 354s in Items; a single-row reply may arrive as
	// the batch Message itself. Handle both.
	if b.Command == "354" {
		c.applyWhoxRow(b.Params)
	}
	for _, item := range b.Items {
		if item == nil {
			continue
		}
		if item.Command == "354" {
			c.applyWhoxRow(item.Params)
		}
	}
}

// handleWhoxReply folds a RPL_WHOSPCRPL (354) row from an unlabeled roster WHO
// into the roster. Replies whose token doesn't match ours (a user-initiated WHO)
// are ignored. Used only when labeled-response is unavailable.
func (c *IRCClient) handleWhoxReply(e ircmsg.Message) {
	if len(e.Params) < 9 || e.Params[1] != whoxRosterToken {
		return
	}
	c.applyWhoxRow(e.Params)
}

// applyWhoxRow parses one 354 row and folds it into the live roster. Params match
// the "%tcuhnfar" request:
//
//	[ourNick, token, channel, user, host, nick, flags, account, realname]
func (c *IRCClient) applyWhoxRow(params []string) {
	if len(params) < 9 {
		return
	}
	user := params[3]
	host := params[4]
	nick := params[5]
	flags := params[6]
	account := params[7]
	realname := params[8]

	// WHOX uses "0" (ircu) or "*" for "no account".
	if account == "0" || account == "*" {
		account = ""
	}
	// The flags field begins with H (here) or G (gone/away).
	away := len(flags) > 0 && flags[0] == 'G'

	// Bot mode: the BOT= letter appears within the WHO flags field (after the
	// here/gone marker and any '*' oper flag), so scan the whole field — not just
	// index 0. Recognizes a bot the instant we WHOX a channel, before it speaks.
	c.mu.RLock()
	botChar := c.serverCapabilities.BotModeChar
	c.mu.RUnlock()
	if botChar != 0 && strings.ContainsRune(flags, botChar) {
		c.markBot(nick)
	}

	userhost := ""
	if user != "" && host != "" {
		userhost = user + "@" + host
	}

	c.applyUserMeta(nick, func(m *UserMeta) {
		m.Away = away
		m.Account = account
		if userhost != "" {
			m.Host = userhost
		}
		if realname != "" {
			m.Realname = realname
		}
	})
}

// monitorSupported reports whether the server advertised MONITOR in ISUPPORT.
func (c *IRCClient) monitorSupported() bool {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.supportsMonitor
}

// serviceNicks are the well-known network-services pseudo-clients. Presence is
// meaningless for them (they're effectively always online), so we never spend a
// MONITOR slot tracking a service even when a PM with one is open.
var serviceNicks = map[string]bool{
	"nickserv": true, "chanserv": true, "saslserv": true, "memoserv": true,
	"hostserv": true, "operserv": true, "botserv": true, "global": true,
}

// IsServiceNick reports whether nick is a well-known network service (NickServ,
// ChanServ, …). Comparison is case-insensitive.
func IsServiceNick(nick string) bool {
	return serviceNicks[strings.ToLower(nick)]
}

// parseMonitorLimit interprets an ISUPPORT token. ok is true when the token is
// MONITOR (announcing presence support); limit is the advertised maximum number
// of monitored targets, or 0 when none/malformed (treated as unlimited).
func parseMonitorLimit(param string) (limit int, ok bool) {
	if param == "MONITOR" {
		return 0, true
	}
	if !strings.HasPrefix(param, "MONITOR=") {
		return 0, false
	}
	n, err := strconv.Atoi(param[len("MONITOR="):])
	if err != nil || n < 0 {
		return 0, true
	}
	return n, true
}

// desiredMonitorState is the single rule deciding whether nick should be on the
// server MONITOR list: an explicit (durable) buddy is always monitored; anyone
// with an open PM is monitored too, except yourself and service pseudo-clients.
func desiredMonitorState(nick, selfNick string, isBuddy, hasOpenPM bool) bool {
	if isBuddy {
		return true
	}
	return hasOpenPM && !strings.EqualFold(nick, selfNick) && !IsServiceNick(nick)
}

// MonitorReconcileNick brings nick's MONITOR state in line with the union rule:
// it is monitored iff it is a durable buddy or has an open PM (and is neither
// ourselves nor a service nick). Call it after a buddy add/remove or a PM
// open/close; it is safe to call when the server lacks MONITOR (no-op).
func (c *IRCClient) MonitorReconcileNick(nick string) {
	if nick == "" || !c.monitorSupported() {
		return
	}
	isBuddy := false
	if nicks, err := c.storage.GetMonitoredNicks(c.networkID); err == nil {
		for _, n := range nicks {
			if c.sameName(n, nick) {
				isBuddy = true
				break
			}
		}
	}
	hasOpenPM := false
	if open, err := c.storage.GetPrivateMessageConversations(c.networkID, c.network.Nickname, true); err == nil {
		for _, u := range open {
			if c.sameName(u, nick) {
				hasOpenPM = true
				break
			}
		}
	}
	c.MonitorSet(nick, desiredMonitorState(nick, c.network.Nickname, isBuddy, hasOpenPM))
}

// MonitorSet arms (on) or disarms (off) nick on the server MONITOR list, keeping
// the armed-set in sync. It is idempotent and a no-op when the desired state
// already holds or the server lacks MONITOR. Adds are skipped once the server's
// advertised MONITOR limit is reached — durable buddies are armed before PM
// correspondents (see sendInitialMonitor), so buddies win the available slots.
func (c *IRCClient) MonitorSet(nick string, on bool) {
	if nick == "" || !c.monitorSupported() {
		return
	}
	key := strings.ToLower(nick)
	c.monitorMu.Lock()
	armed := c.monitorArmed[key]
	atLimit := c.monitorLimit > 0 && len(c.monitorArmed) >= c.monitorLimit
	c.monitorMu.Unlock()
	if on == armed {
		return // already in the desired state
	}
	if on {
		if atLimit {
			logger.Log.Debug().Str("nick", nick).Int("limit", c.monitorLimit).
				Msg("MONITOR list full; skipping add (presence will show as unknown)")
			return
		}
		if err := c.MonitorAdd(nick); err != nil {
			logger.Log.Debug().Err(err).Str("nick", nick).Msg("MONITOR + send failed")
		}
		return
	}
	if err := c.MonitorRemove(nick); err != nil {
		logger.Log.Debug().Err(err).Str("nick", nick).Msg("MONITOR - send failed")
	}
}

// MonitorAdd asks the server to track a nick's presence (MONITOR + nick) and
// records it in the armed-set. The online/offline result arrives asynchronously
// via 730/731.
func (c *IRCClient) MonitorAdd(nick string) error {
	if nick == "" {
		return fmt.Errorf("monitor nick required")
	}
	c.monitorMu.Lock()
	c.monitorArmed[strings.ToLower(nick)] = true
	c.monitorMu.Unlock()
	if c.conn == nil {
		return nil // not connected yet; armed-set re-sent on connect
	}
	return c.conn.SendRaw("MONITOR + " + nick)
}

// MonitorRemove stops tracking a nick (MONITOR - nick) and drops its cached state.
func (c *IRCClient) MonitorRemove(nick string) error {
	if nick == "" {
		return fmt.Errorf("monitor nick required")
	}
	key := strings.ToLower(nick)
	c.monitorMu.Lock()
	delete(c.monitorStatus, key)
	delete(c.monitorArmed, key)
	c.monitorMu.Unlock()
	if c.conn == nil {
		return nil
	}
	return c.conn.SendRaw("MONITOR - " + nick)
}

// sendInitialMonitor re-arms the server-side MONITOR list on (re)connect from the
// union of durable buddies and open PM correspondents (excluding ourselves and
// service nicks). Buddies are listed first so they keep their slots if the
// server's advertised limit is tight. No-op when the server lacks MONITOR. Nicks
// are sent comma-separated in chunks to respect line length.
func (c *IRCClient) sendInitialMonitor() {
	if !c.monitorSupported() {
		return
	}
	buddies, err := c.storage.GetMonitoredNicks(c.networkID)
	if err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to load monitored nicks")
	}
	open, err := c.storage.GetPrivateMessageConversations(c.networkID, c.network.Nickname, true)
	if err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to load open PM conversations for MONITOR")
	}

	// Build the ordered, de-duplicated union: buddies first, then open PMs.
	seen := make(map[string]bool)
	var armed []string
	add := func(nick string, isBuddy, hasOpenPM bool) {
		key := strings.ToLower(nick)
		if seen[key] || !desiredMonitorState(nick, c.network.Nickname, isBuddy, hasOpenPM) {
			return
		}
		seen[key] = true
		armed = append(armed, nick)
	}
	for _, n := range buddies {
		add(n, true, false)
	}
	for _, u := range open {
		add(u, false, true)
	}

	// Respect the server's advertised MONITOR limit (buddies-first ordering means
	// they survive the truncation).
	c.mu.RLock()
	limit := c.monitorLimit
	c.mu.RUnlock()
	if limit > 0 && len(armed) > limit {
		armed = armed[:limit]
	}

	// Record the armed-set so it matches the fresh (empty) server list.
	c.monitorMu.Lock()
	c.monitorArmed = make(map[string]bool, len(armed))
	for _, n := range armed {
		c.monitorArmed[strings.ToLower(n)] = true
	}
	c.monitorMu.Unlock()

	if c.conn == nil {
		return
	}
	const chunkSize = 20
	for i := 0; i < len(armed); i += chunkSize {
		end := i + chunkSize
		if end > len(armed) {
			end = len(armed)
		}
		if err := c.conn.SendRaw("MONITOR + " + strings.Join(armed[i:end], ",")); err != nil {
			logger.Log.Warn().Err(err).Msg("Failed to send initial MONITOR list")
			return
		}
	}
}

// setMonitorPresence records a monitored nick's online state and emits
// EventMonitorChanged on a real change (idempotent, like applyUserMeta).
func (c *IRCClient) setMonitorPresence(nick string, online bool) {
	if nick == "" {
		return
	}
	key := strings.ToLower(nick)
	c.monitorMu.Lock()
	prev, existed := c.monitorStatus[key]
	if existed && prev == online {
		c.monitorMu.Unlock()
		return
	}
	c.monitorStatus[key] = online
	c.monitorMu.Unlock()

	c.eventBus.Emit(events.Event{
		Type: EventMonitorChanged,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"nickname":  nick,
			"online":    online,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// handleMonitorPresence parses a 730 (RPL_MONONLINE) or 731 (RPL_MONOFFLINE)
// reply, whose second parameter is a comma-separated target list ("nick!user@host"
// or bare nick), and updates each nick's presence.
func (c *IRCClient) handleMonitorPresence(e ircmsg.Message, online bool) {
	if len(e.Params) < 2 {
		return
	}
	for _, target := range strings.Split(e.Params[1], ",") {
		target = strings.TrimSpace(target)
		if target == "" {
			continue
		}
		nick, _, _ := strings.Cut(target, "!") // strip any !user@host
		c.setMonitorPresence(nick, online)
	}
}

// MonitorPresence returns a snapshot of known monitored-nick presence
// (lowercased nick -> online) for hydrating the frontend on demand.
func (c *IRCClient) MonitorPresence() map[string]bool {
	c.monitorMu.Lock()
	defer c.monitorMu.Unlock()
	out := make(map[string]bool, len(c.monitorStatus))
	for k, v := range c.monitorStatus {
		out[k] = v
	}
	return out
}

// RequestChatHistoryBefore asks the server for up to `limit` messages older than
// beforeISO (an ISO8601 timestamp, e.g. 2024-06-14T00:00:00.000Z) for target.
// Used for scroll-to-top deep backscroll.
func (c *IRCClient) RequestChatHistoryBefore(target, beforeISO string, limit int) error {
	if !c.chatHistoryEnabled() {
		return fmt.Errorf("server does not support chathistory")
	}
	if target == "" || beforeISO == "" {
		return fmt.Errorf("chathistory target and timestamp required")
	}
	limit = c.clampChatHistoryLimit(limit)
	return c.conn.SendRaw(fmt.Sprintf("CHATHISTORY BEFORE %s timestamp=%s %d", target, beforeISO, limit))
}

// handleChatHistoryBatch is registered via AddBatchCallback. The ergochat/irc-go
// library collects each "BATCH +id chathistory <target> … -id" group and hands it
// here as one *Batch (b.Items holds the replayed lines). Returning true "claims"
// the batch so the library does NOT flatten it into per-message dispatch — that
// would emit a live message event and re-run notification logic for every replayed
// line. Non-chathistory batches return false, so the library keeps handling them
// exactly as before this callback existed.
func (c *IRCClient) handleChatHistoryBatch(b *ircevent.Batch) bool {
	if b == nil {
		return false
	}
	// BATCH start params: <+id> <type> [type-specific params…]. For chathistory the
	// type is at Params[1] and the requested target at Params[2].
	if len(b.Params) < 2 || b.Params[1] != "chathistory" {
		return false
	}
	target := ""
	if len(b.Params) >= 3 {
		target = b.Params[2]
	}

	msgs := make([]storage.Message, 0, len(b.Items))
	for _, item := range b.Items {
		if item == nil {
			continue
		}
		if msg, ok := c.buildHistoryMessage(item.Message); ok {
			msgs = append(msgs, msg)
		}
	}

	inserted := 0
	if len(msgs) > 0 {
		n, err := c.storage.WriteHistoryMessages(msgs)
		if err != nil {
			logger.Log.Error().Err(err).Str("target", target).Msg("Failed to store chathistory batch")
		} else {
			inserted = n
		}
	}

	c.eventBus.Emit(events.Event{
		Type: EventHistoryReceived,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"target":    target,
			"inserted":  inserted,
			"returned":  len(msgs),
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})

	return true
}

// buildHistoryMessage converts a single replayed PRIVMSG/NOTICE line from a
// CHATHISTORY batch into a storage.Message, applying the same channel-vs-PM
// routing as the live handlers. ok=false for lines we don't persist (malformed,
// or non-ACTION CTCP).
func (c *IRCClient) buildHistoryMessage(e ircmsg.Message) (storage.Message, bool) {
	if len(e.Params) < 2 {
		return storage.Message{}, false
	}
	target := e.Params[0]
	text := e.Params[1]
	user := e.Nick()

	messageType := "privmsg"
	switch e.Command {
	case "PRIVMSG":
		// CTCP ACTION -> action (stored like the live handler); other CTCP skipped.
		if len(text) >= 2 && text[0] == '\001' && text[len(text)-1] == '\001' {
			parts := strings.Fields(text[1 : len(text)-1])
			if len(parts) > 0 && strings.ToUpper(parts[0]) == "ACTION" {
				messageType = "action"
				text = fmt.Sprintf("* %s %s", user, strings.Join(parts[1:], " "))
			} else {
				return storage.Message{}, false
			}
		}
	case "NOTICE":
		messageType = "notice"
	default:
		return storage.Message{}, false
	}

	var channelID *int64
	var pmTarget string
	if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
		if ch, err := c.storage.GetChannelByName(c.networkID, target); err == nil {
			channelID = &ch.ID
		}
	} else {
		pmTarget = c.pmPeer(user, target)
		if _, err := c.storage.GetOrCreatePMConversation(c.networkID, pmTarget, c.network.Nickname); err != nil {
			logger.Log.Error().Err(err).Str("pmTarget", pmTarget).Msg("Failed to create/get PM conversation for history")
		}
	}

	rawLine, _ := e.Line()
	return storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   channelID,
		User:        user,
		Message:     text,
		MessageType: messageType,
		Timestamp:   c.getHistoryTime(e),
		RawLine:     rawLine,
		PMTarget:    pmTarget,
		MsgID:       c.getMsgID(e),
	}, true
}

// handleSTSAdvertisement reacts to an `sts` capability seen in CAP LS. It parses
// the policy and emits EventSTSPolicy for the App to act on; the protocol layer
// deliberately does not perform the reconnect or persistence itself (those need
// connection-lifecycle and storage access the App owns). The `secure` field tells
// the App which half of the spec applies: over plaintext only the port is honored
// (as an upgrade target), over TLS the duration is trusted and persisted.
//
// Per the spec, STS never applies to a connection made to an IP literal, so those
// are dropped here before any event is emitted.
func (c *IRCClient) handleSTSAdvertisement(value string) {
	if IsIPLiteral(c.network.Address) {
		return
	}
	p := parseSTS(value)

	secure := c.network.TLS
	var msg string
	if secure {
		if p.Duration == 0 {
			msg = "Server cleared its STS policy (duration=0)"
		} else {
			// The policy's secure port is the advertised port, or the port we're already
			// connected on when the directive is omitted (valid over TLS).
			port := p.Port
			if port <= 0 {
				port = c.network.Port
			}
			msg = fmt.Sprintf("Server advertised STS policy: enforce TLS for %d seconds (port %d)", p.Duration, port)
		}
	} else if p.Port > 0 {
		msg = fmt.Sprintf("Server advertised STS policy: upgrading to TLS on port %d", p.Port)
	} else {
		// Plaintext advertisement with no port is not actionable.
		return
	}

	c.storage.WriteMessage(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     msg,
		MessageType: "status",
		Timestamp:   time.Now(),
	})

	c.eventBus.Emit(events.Event{
		Type: EventSTSPolicy,
		Data: map[string]interface{}{
			"networkId":   c.networkID,
			"network":     c.network.Address,
			"host":        c.network.Address,
			"port":        p.Port,
			"currentPort": c.network.Port,
			"duration":    p.Duration,
			"secure":      secure,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// accumulateCapLS appends one CAP LS line's capability tokens to buf and reports
// whether the advertisement is complete. CAP LS 302 lets a server split a long
// list across lines: non-final lines carry a "*" continuation marker, so their
// params are [nick, "LS", "*", caps] and the cap tokens live in params[3]; a
// single or final line is [nick, "LS", caps] with the tokens in params[2]. The
// tokens are always the FINAL param — reading params[2] unconditionally would
// drop every capability on a continuation line (there params[2] is the literal
// "*"). Returns (fullList, true) only on the final line; ("", false) otherwise.
func accumulateCapLS(params []string, buf *strings.Builder) (string, bool) {
	if len(params) < 3 {
		return "", false
	}
	continuation := len(params) > 3 && params[2] == "*"
	tokens := params[2]
	if continuation {
		tokens = params[3]
	}
	buf.WriteString(tokens)
	buf.WriteString(" ")
	if continuation {
		return "", false
	}
	return buf.String(), true
}

func contains(capabilities, cap string) bool {
	// Split by space and check each capability
	// Capabilities can be in the form "cap" or "cap=value" or "cap=value1,value2"
	caps := strings.Fields(capabilities)
	for _, c := range caps {
		// Check if the capability name matches (before the = sign)
		capName := c
		if idx := strings.Index(c, "="); idx != -1 {
			capName = c[:idx]
		}
		if capName == cap {
			return true
		}
	}
	return false
}

// capValue returns the value advertised for a capability in a CAP LS list
// (the part after "=", e.g. "draft/chathistory=50" -> "50"), and whether the
// capability was present at all. An empty string with present=true means the
// capability was advertised without a value.
func capValue(capabilities, cap string) (value string, present bool) {
	for _, c := range strings.Fields(capabilities) {
		name := c
		val := ""
		if idx := strings.Index(c, "="); idx != -1 {
			name = c[:idx]
			val = c[idx+1:]
		}
		if name == cap {
			return val, true
		}
	}
	return "", false
}

// endCapNegotiation marks the initial CAP handshake complete and sends CAP END.
// Setting capNegotiationDone lets the ACK handler tell a registration ACK (which
// must be followed by CAP END) apart from a post-registration ACK triggered by a
// CAP NEW request (which must NOT send CAP END again).
// It is idempotent: if CAP negotiation is already done it returns without sending
// a second CAP END. This matters because RPL_LOGGEDIN (900) and RPL_SASLSUCCESS
// (903) commonly both arrive and both call handleSASLSuccess -> endCapNegotiation.
func (c *IRCClient) endCapNegotiation() {
	c.mu.Lock()
	if c.capNegotiationDone {
		c.mu.Unlock()
		return
	}
	c.capNegotiationDone = true
	c.mu.Unlock()
	c.conn.SendRaw("CAP END")
}

// capsToRequest returns the subset of requestedCaps that the server has offered
// and we don't already have enabled. sasl is included only during the initial
// handshake (includeSASL); CAP NEW never re-runs SASL, so callers pass false
// there. The result preserves requestedCaps order for stable CAP REQ output.
func capsToRequest(offered string, enabled map[string]bool, includeSASL bool) []string {
	var toRequest []string
	for _, wantedCap := range requestedCaps {
		if wantedCap == "sasl" && !includeSASL {
			continue
		}
		if enabled[wantedCap] {
			continue
		}
		if contains(offered, wantedCap) {
			toRequest = append(toRequest, wantedCap)
		}
	}
	return toRequest
}

// startSASLAuth initiates SASL authentication
func (c *IRCClient) startSASLAuth() {
	c.mu.Lock()
	if c.saslInProgress {
		c.mu.Unlock()
		return
	}
	c.saslInProgress = true
	c.mu.Unlock()

	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     fmt.Sprintf("Starting SASL authentication with mechanism: %s", c.saslMechanism),
		MessageType: "status",
		Timestamp:   time.Now(),
		RawLine:     "",
	}
	c.storage.WriteMessage(statusMsg)

	c.eventBus.Emit(events.Event{
		Type:      EventSASLStarted,
		Data:      map[string]interface{}{"network": c.network.Address, "mechanism": c.saslMechanism},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})

	// Send AUTHENTICATE with mechanism
	c.conn.SendRaw(fmt.Sprintf("AUTHENTICATE %s", c.saslMechanism))
}

// handleAUTHENTICATE handles AUTHENTICATE command responses
func (c *IRCClient) handleAUTHENTICATE(e ircmsg.Message) {
	if len(e.Params) == 0 {
		return
	}

	response := e.Params[0]

	switch c.saslMechanism {
	case "PLAIN":
		c.handlePLAINAuth(response)
	case "EXTERNAL":
		c.handleEXTERNALAuth(response)
	case "SCRAM-SHA-256", "SCRAM-SHA-512":
		c.handleSCRAMAuth(response)
	default:
		// Unknown mechanism
		c.conn.SendRaw("AUTHENTICATE *")
		c.mu.Lock()
		c.saslInProgress = false
		c.mu.Unlock()
	}
}

// handlePLAINAuth handles PLAIN mechanism authentication
func (c *IRCClient) handlePLAINAuth(response string) {
	if response == "+" {
		// Server is ready for credentials
		// PLAIN format: \0username\0password (base64 encoded)
		authString := fmt.Sprintf("\x00%s\x00%s", c.saslUsername, c.saslPassword)
		encoded := base64.StdEncoding.EncodeToString([]byte(authString))
		c.conn.SendRaw(fmt.Sprintf("AUTHENTICATE %s", encoded))
	} else if response == "*" {
		// Abort
		c.mu.Lock()
		c.saslInProgress = false
		c.mu.Unlock()
		c.handleSASLFailure("PLAIN authentication aborted by server")
	}
}

// handleEXTERNALAuth handles EXTERNAL mechanism authentication
func (c *IRCClient) handleEXTERNALAuth(response string) {
	if response == "+" {
		// EXTERNAL uses TLS client certificate, send empty response
		c.conn.SendRaw("AUTHENTICATE +")
	} else if response == "*" {
		// Abort
		c.mu.Lock()
		c.saslInProgress = false
		c.mu.Unlock()
		c.handleSASLFailure("EXTERNAL authentication aborted by server")
	}
}

// handleSCRAMAuth handles SCRAM-SHA-256 and SCRAM-SHA-512 authentication
// Implementation is in scram.go

// Connect connects to the IRC server
func (c *IRCClient) Connect() error {
	// CAP LS 302 is sent after Connect() succeeds but before Loop()

	if err := c.conn.Connect(); err != nil {
		c.eventBus.Emit(events.Event{
			Type:      EventError,
			Data:      map[string]interface{}{"error": err.Error()},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
		return fmt.Errorf("failed to connect: %w", err)
	}

	// Reset capability state before negotiation so a reused client (reconnect)
	// renegotiates from a clean slate. This runs before Loop() starts, so no CAP
	// response can have been processed yet — unlike the post-registration connect
	// callback (which fires on RPL_ENDOFMOTD), this point is race-free.
	c.mu.Lock()
	c.enabledCaps = make(map[string]bool)
	c.capNegotiationDone = false
	c.mu.Unlock()

	// Always send CAP LS 302 for IRCv3 capability negotiation
	c.conn.SendRaw("CAP LS 302")
	c.storage.WriteMessage(storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        "*",
		Message:     "Sent CAP LS 302 for IRCv3 capability negotiation",
		MessageType: "status",
		Timestamp:   time.Now(),
	})

	// Start the connection loop in a goroutine
	c.runLoop()

	// Auto-join is gated on registration completion, not a fixed timer: JOIN is
	// what makes the server send the NAMES list that builds the roster, and a
	// JOIN sent before RPL_WELCOME (001) is silently dropped. The end-of-MOTD
	// handlers (376/422) and a fallback timer armed at 001 both call
	// triggerAutoJoin, which fires doAutoJoin exactly once via autoJoinOnce. We
	// reset the Once here so each Connect (including auto-reconnect) re-arms it.
	c.mu.Lock()
	c.autoJoinOnce = &sync.Once{}
	c.mu.Unlock()

	return nil
}

// triggerAutoJoin runs the one auto-join for this connection, guarded by
// autoJoinOnce so the multiple registration signals that race to call it
// (RPL_ENDOFMOTD 376, ERR_NOMOTD 422, and the 001-armed fallback timer) result
// in exactly one join pass.
func (c *IRCClient) triggerAutoJoin() {
	c.mu.RLock()
	once := c.autoJoinOnce
	action := c.autoJoinAction
	c.mu.RUnlock()
	if once == nil || action == nil {
		return
	}
	once.Do(func() { go action() })
}

// doAutoJoin joins the channels for this connection. On a fresh startup we join
// only auto_join channels; on an auto-reconnect we rejoin every channel we were
// in so the server resends NAMES (see channelsToJoin). Each JOIN goes through
// the rate limiter because rejoining a full session at once would otherwise
// burst dozens of commands and trip the server's flood protection — the
// underlying library does no throttling itself.
func (c *IRCClient) doAutoJoin() {
	c.mu.RLock()
	reconnect := c.reconnecting
	c.mu.RUnlock()

	// Re-arm the server-side MONITOR list from the durable buddy list now that
	// registration is complete (same trigger as auto-join).
	c.sendInitialMonitor()

	// Announce ourselves as a bot if configured (gated on server BOT= support).
	c.announceBotMode()

	channels, err := c.channelsToJoin(reconnect)
	if err != nil {
		logger.Log.Error().Err(err).Bool("reconnect", reconnect).Msg("Failed to get channels to join")
		return
	}
	logger.Log.Info().Int("count", len(channels)).Bool("reconnect", reconnect).Msg("Joining channels")
	for _, channel := range channels {
		c.rateLimiter.Wait()
		logger.Log.Info().Str("channel", channel.Name).Bool("reconnect", reconnect).Msg("Joining channel")
		if err := c.conn.Join(channel.Name); err != nil {
			logger.Log.Error().Err(err).Str("channel", channel.Name).Msg("Failed to join channel")
		}
	}
}

// announceBotMode marks this connection as a bot when the network is configured
// to (IdentifyAsBot) and the server advertised a BOT= letter. It is called once
// per connection from doAutoJoin, alongside auto-join and the MONITOR re-arm, so
// it re-asserts on every (re)connect. When the setting is on but the server has
// no bot mode, it writes a single warning status line instead of failing silently.
func (c *IRCClient) announceBotMode() {
	if !c.network.IdentifyAsBot {
		return
	}
	letter := c.BotMode()
	if letter == "" {
		c.writeStatusLine("warning", "Server does not support bot mode; +B not set")
		return
	}
	nick := c.CurrentNick()
	if err := c.conn.SendRaw(fmt.Sprintf("MODE %s +%s", nick, letter)); err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to send bot mode announcement")
	}
}

// Disconnect disconnects from the IRC server
// runLoop launches the library's connection Loop in a goroutine and records a
// done channel that closes when that goroutine fully exits. Teardown waits on it
// so a follow-on reconnect can't race a socket that is still being torn down.
//
// This matters because the library's Loop auto-reconnects on ANY drop unless its
// quit flag is set — and ReconnectFreq=0 does NOT disable that (the library
// silently defaults it to two minutes). A teardown that fails to set quit would
// therefore leak this goroutine as a ghost that reconnects under a fallback nick
// and holds the preferred nick hostage, which is exactly the bug this guards.
func (c *IRCClient) runLoop() {
	done := make(chan struct{})
	c.mu.Lock()
	c.loopDone = done
	c.mu.Unlock()
	go func() {
		defer close(done)
		c.conn.Loop()
	}()
}

// Disconnect tears the connection down deterministically. It does NOT merely send
// a raw QUIT: it drives the library's Quit(), which both sends the QUIT and sets
// the library's quit flag. That flag is the only thing that stops Loop() from
// auto-reconnecting on the resulting socket close (ReconnectFreq=0 does not
// disable it — see runLoop). Without it a "disconnected" client leaks a Loop
// goroutine that reconnects under a fallback nick and holds the preferred nick
// hostage — the ghost-session bug. After signalling quit it waits, bounded, for
// the Loop goroutine to fully exit so a follow-on reconnect can't race a socket
// that is still closing.
//
// Idempotent and safe on an already-dropped client (connected == false): the
// quit flag must still be set there, because a dropped client's Loop is the one
// about to reconnect. The mutex is released before the wait — the Loop's own
// disconnect callback acquires it on the way out.
// signalQuit puts the library connection into the quitting state — it sends a
// QUIT and, crucially, sets the library's quit flag, which is the ONLY thing
// that stops Loop() from auto-reconnecting on the resulting socket close (see
// runLoop). It marks us disconnected and returns the loopDone channel so callers
// that want a deterministic teardown can wait on it. It never waits itself, so
// it is safe to call from inside the Loop's own callbacks (where waiting on
// loopDone would deadlock). Idempotent.
func (c *IRCClient) signalQuit(reason string) <-chan struct{} {
	c.mu.Lock()
	conn := c.conn
	done := c.loopDone
	c.connected = false
	// Sticky: this client is being torn down. The library Loop checks its quit
	// flag only at the top of its iteration, not after the reconnect delay, so a
	// Quit() that lands mid-cycle still lets it complete one more Connect(). The
	// abandoned flag makes that final connect callback a no-op (see onConnect) so
	// the doomed socket can't re-register, steal the nick, or report itself
	// connected. Never cleared — an abandoned client is gone for good; a real
	// reconnect always uses a fresh IRCClient.
	c.abandoned = true
	c.mu.Unlock()

	if conn == nil {
		return nil
	}
	conn.QuitMessage = reason
	conn.Quit()
	return done
}

// StopReconnect stops a connection we are abandoning WITHOUT immediately
// re-dialing (a drop we have decided not to reconnect, e.g. AutoConnect off). It
// sets the library quit flag so the orphaned Loop goroutine cannot silently
// reconnect minutes later as a ghost holding our nick. It does not wait — the
// quit flag alone prevents the reconnect; there is no follow-on dial to race.
func (c *IRCClient) StopReconnect() {
	c.signalQuit("Disconnecting")
}

// Disconnect tears the connection down deterministically: it signals quit (so the
// library Loop cannot auto-reconnect as a ghost — the bug this guards) and then
// waits, bounded, for that Loop goroutine to fully exit so a follow-on reconnect
// can't race a socket that is still closing. Safe and idempotent on an
// already-dropped client. The mutex is not held across the wait — the Loop's own
// disconnect callback acquires it on the way out.
func (c *IRCClient) Disconnect() error {
	done := c.signalQuit("Disconnecting")

	if done != nil {
		select {
		case <-done:
		case <-time.After(constants.ConnectionTeardownTimeout):
			logger.Log.Warn().Int64("network_id", c.networkID).Msg("Disconnect: timed out waiting for connection loop to exit; quit flag is set so it will not reconnect")
		}
	}
	return nil
}

// validTypingState reports whether s is one of the IRCv3 +typing client-tag
// values (ircv3.net/specs/client-tags/typing). Shared by the send and receive
// paths so an unknown value is rejected consistently in both directions.
func validTypingState(s string) bool {
	switch s {
	case "active", "paused", "done":
		return true
	default:
		return false
	}
}

// SendTyping emits an IRCv3 +typing client tag for target as a TAGMSG. Typing
// is best-effort and ephemeral: if message-tags was not negotiated we silently
// no-op (the server would not relay the client tag anyway). An invalid state is
// a programming error and is rejected. The frontend owns the throttle/idle/done
// state machine; this method is a thin relay (mirrors SendMessage's guards).
func (c *IRCClient) SendTyping(target, state string) error {
	if !validTypingState(state) {
		return fmt.Errorf("invalid typing state %q", state)
	}
	if !c.capEnabled("message-tags") {
		return nil
	}

	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	c.rateLimiter.Wait()
	if err := c.conn.SendWithTags(map[string]string{"+typing": state}, "TAGMSG", target); err != nil {
		return fmt.Errorf("failed to send typing tag: %w", err)
	}
	return nil
}

// handleTypingTag processes an inbound TAGMSG carrying the IRCv3 +typing client
// tag. It drops our own echo (echo-message can reflect our TAGMSG back), ignores
// unknown/absent states, and emits EventTypingReceived for the frontend. The
// conversation target is the channel for channel-addressed tags, or the sender
// for a tag addressed to us by nick (a PM). Nothing is persisted — typing state
// is purely ephemeral.
func (c *IRCClient) handleTypingTag(e ircmsg.Message) {
	present, state := e.GetTag("+typing")
	if !present || !validTypingState(state) {
		return
	}
	nick := e.Nick()
	if c.isMe(nick) {
		return
	}
	if len(e.Params) < 1 {
		return
	}
	dest := e.Params[0]

	// Channel-addressed: the conversation is the channel. Otherwise the tag was
	// addressed to us by nick, so the conversation is the sender (the PM peer).
	target := nick
	if len(dest) > 0 && (dest[0] == '#' || dest[0] == '&') {
		target = dest
	}

	c.eventBus.Emit(events.Event{
		Type: EventTypingReceived,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"target":    target,
			"nick":      nick,
			"state":     state,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})
}

// SendMessage sends a message to a channel or user
func (c *IRCClient) SendMessage(target, message string) error {
	return c.sendMessage(target, message, "", "")
}

// SendMessageWithTags sends a message with optional IRCv3 client tags.
// replyMsgID populates +draft/reply; channelContext populates
// +draft/channel-context. Either may be empty.
func (c *IRCClient) SendMessageWithTags(target, message, replyMsgID, channelContext string) error {
	return c.sendMessage(target, message, replyMsgID, channelContext)
}

// sendMessage is the shared core for SendMessage and SendMessageWithTags.
func (c *IRCClient) sendMessage(target, message, replyMsgID, channelContext string) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	// Wait for rate limiter before sending
	c.rateLimiter.Wait()

	if tags := buildSendTags(replyMsgID, channelContext); tags != nil {
		if err := c.conn.SendWithTags(tags, "PRIVMSG", target, message); err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
	} else {
		if err := c.conn.Privmsg(target, message); err != nil {
			return fmt.Errorf("failed to send message: %w", err)
		}
	}

	// Determine if it's a channel or private message. For a PM the peer is the
	// recipient (we are the sender); channel messages have no PM peer.
	var channelID *int64
	var pmTarget string
	if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
		// Channel message - look up channel ID
		ch, err := c.storage.GetChannelByName(c.networkID, target)
		if err == nil {
			channelID = &ch.ID
		} else {
			// Channel not found in database - might be a channel we haven't joined yet
			// Store with nil channel_id for now
			logger.Log.Debug().Err(err).Str("target", target).Msg("Channel not found in database")
		}
	} else {
		// Private message - create or get PM conversation
		pmTarget = target
		_, err := c.storage.GetOrCreatePMConversation(c.networkID, target, c.network.Nickname)
		if err != nil {
			logger.Log.Error().Err(err).Str("target", target).Msg("Failed to create/get PM conversation")
		}
	}

	// When echo-message is enabled, the server will echo our message back and
	// we store it in the PRIVMSG handler (canonical copy with server timestamp).
	// Otherwise, store it immediately ourselves.
	c.mu.RLock()
	hasEcho := c.enabledCaps["echo-message"]
	c.mu.RUnlock()

	if !hasEcho {
		msg := storage.Message{
			NetworkID:      c.networkID,
			ChannelID:      channelID,
			User:           c.network.Nickname,
			Message:        message,
			MessageType:    "privmsg",
			Timestamp:      time.Now(),
			RawLine:        fmt.Sprintf("PRIVMSG %s :%s", target, message),
			PMTarget:       pmTarget,
			ReplyMsgID:     replyMsgID,
			ChannelContext: channelContext,
		}
		if err := c.storage.WriteMessageSync(msg); err != nil {
			return fmt.Errorf("failed to store message: %w", err)
		}
	}

	// Emit event
	c.eventBus.Emit(events.Event{
		Type: EventMessageSent,
		Data: map[string]interface{}{
			"network":   c.network.Address,
			"networkId": c.networkID,
			"target":    target,
			"message":   message,
			"pmTarget":  pmTarget,
		},
		Timestamp: time.Now(),
		Source:    events.EventSourceIRC,
	})

	return nil
}

// SetReconnecting marks whether this connection is an auto-reconnect after an
// unexpected drop. It must be set before Connect() so the auto-join goroutine
// can choose the correct rejoin set. See channelsToJoin.
func (c *IRCClient) SetReconnecting(v bool) {
	c.mu.Lock()
	c.reconnecting = v
	c.mu.Unlock()
}

// isChannelName reports whether name is an IRC channel (vs. a nick/PM target),
// honoring the server's advertised CHANTYPES (RPL_ISUPPORT) and falling back to
// the conventional "#&" before the server advertises one.
func (c *IRCClient) isChannelName(name string) bool {
	return channelNameMatches(name, c.ChanTypes())
}

// ChanTypes returns the server-advertised CHANTYPES set, or the default "#&"
// when the server hasn't advertised one yet. Exported so the App can surface it
// to the frontend (ServerCapabilitiesInfo). Lock-free.
func (c *IRCClient) ChanTypes() string {
	if p := c.chanTypesAtomic.Load(); p != nil && *p != "" {
		return *p
	}
	return defaultChanTypes
}

// CaseMapping returns the server-advertised CASEMAPPING, or "rfc1459" (the
// protocol default when the server says nothing). Exported so the App can
// surface it to the frontend (ServerCapabilitiesInfo). Lock-free.
func (c *IRCClient) CaseMapping() string {
	if p := c.caseMappingAtomic.Load(); p != nil && *p != "" {
		return *p
	}
	return "rfc1459"
}

// sameName reports whether two nicks (or channels) are equal under the server's
// CASEMAPPING. Use this for identity checks instead of strings.EqualFold, which
// applies Unicode folding the server doesn't and misses the rfc1459 []\~ ↔ {}|^
// pairing.
func (c *IRCClient) sameName(a, b string) bool {
	m := c.CaseMapping()
	return casefold(m, a) == casefold(m, b)
}

// channelsToJoin returns the channels the auto-join goroutine should JOIN on this
// connection.
//
//   - On a fresh startup connect (reconnect == false) we honor the user's sticky
//     preference and join only channels with auto_join enabled.
//   - On an auto-reconnect after an unexpected drop (reconnect == true) we restore
//     the session we lost by rejoining every channel we were actually in — those
//     marked open or where we're still a tracked member (GetOpenChannels). This is
//     what makes the server resend NAMES so nick lists refresh; gating reconnect on
//     auto_join is what left most channels stale after sleep/wake.
func (c *IRCClient) channelsToJoin(reconnect bool) ([]storage.Channel, error) {
	if reconnect {
		open, err := c.storage.GetOpenChannels(c.networkID, c.network.Nickname)
		if err != nil {
			return nil, err
		}
		result := make([]storage.Channel, 0, len(open))
		for _, ch := range open {
			if c.isChannelName(ch.Name) {
				result = append(result, ch)
			}
		}
		return result, nil
	}

	all, err := c.storage.GetChannels(c.networkID)
	if err != nil {
		return nil, err
	}
	result := make([]storage.Channel, 0, len(all))
	for _, ch := range all {
		if ch.AutoJoin && c.isChannelName(ch.Name) {
			result = append(result, ch)
		}
	}
	return result, nil
}

// JoinChannel joins an IRC channel
func (c *IRCClient) JoinChannel(channel string) error {
	// Validate channel name
	if err := validation.ValidateChannelName(channel); err != nil {
		return fmt.Errorf("invalid channel name: %w", err)
	}

	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		logger.Log.Warn().Str("channel", channel).Msg("Not connected, cannot join")
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	logger.Log.Info().
		Str("channel", channel).
		Str("network", c.network.Name).
		Str("nick", c.network.Nickname).
		Msg("Sending JOIN command for channel")
	if err := c.conn.Join(channel); err != nil {
		logger.Log.Error().Err(err).Msg("Error from ircevent.Join")
		return err
	}
	logger.Log.Info().Str("channel", channel).Msg("JOIN command sent successfully")
	return nil
}

// PartChannel leaves an IRC channel
func (c *IRCClient) PartChannel(channel string) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	return c.conn.Part(channel)
}

// SetNetworkID sets the network ID for message storage
func (c *IRCClient) SetNetworkID(id int64) {
	c.networkID = id
}

// WriteStatusMessage writes a message to the status window
func (c *IRCClient) WriteStatusMessage(message string) error {
	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil, // Status window
		User:        "*",
		Message:     message,
		MessageType: "status",
		Timestamp:   time.Now(),
		RawLine:     "",
	}
	return c.storage.WriteMessage(statusMsg)
}

// SendRawCommand sends a raw IRC command
func (c *IRCClient) SendRawCommand(command string) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	// Wait for rate limiter before sending
	c.rateLimiter.Wait()

	// Send raw command via the connection's SendRaw method
	if err := c.conn.SendRaw(command); err != nil {
		return fmt.Errorf("failed to send raw command: %w", err)
	}

	// Store command in status window
	msg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil, // Status window
		User:        c.network.Nickname,
		Message:     command,
		MessageType: "command",
		Timestamp:   time.Now(),
		RawLine:     command,
	}

	if err := c.storage.WriteMessage(msg); err != nil {
		return fmt.Errorf("failed to store command: %w", err)
	}

	return nil
}

// parsePREFIX parses the PREFIX parameter from ISUPPORT
// Format: (ov)@+ where (ov) are the mode letters and @+ are the prefix characters
// This maps '@' -> 'o' (op) and '+' -> 'v' (voice)
// applyISUPPORTToken parses a single RPL_ISUPPORT (005) token and records it on
// the client's server capabilities. Both valueless tokens (WHOX, UTF8ONLY) and
// key=value tokens (PREFIX, CHANMODES, MONITOR, EXTBAN) are handled here so the
// dispatch is unit-testable independent of the 005 callback wiring.
func (c *IRCClient) applyISUPPORTToken(param string) {
	switch {
	case strings.HasPrefix(param, "PREFIX="):
		c.parsePREFIX(param[len("PREFIX="):])

	case param == "WHOX":
		// Valueless token announcing extended-WHO (354) support.
		c.mu.Lock()
		c.supportsWHOX = true
		c.mu.Unlock()

	case strings.HasPrefix(param, "CHANTYPES="):
		// CHANTYPES=<chars> advertises the channel-prefix characters this server
		// uses (commonly "#", sometimes "#&", "+", "!"). Recording it lets channel
		// detection adapt instead of assuming "#&". An empty value (the server
		// supports no channel types) is stored verbatim so chanTypes() can fall
		// back to the default rather than treating everything as a channel.
		chanTypes := param[len("CHANTYPES="):]
		c.chanTypesAtomic.Store(&chanTypes)
		c.mu.Lock()
		c.serverCapabilities.ChanTypes = chanTypes
		c.mu.Unlock()

	case strings.HasPrefix(param, "CASEMAPPING="):
		// CASEMAPPING=<name> tells us how the server folds case for nick/channel
		// equality ("ascii", "rfc1459", "rfc1459-strict"). We fold the same way so
		// "Nick[a]" and "nick{a}" resolve to one identity on rfc1459 networks.
		caseMapping := param[len("CASEMAPPING="):]
		c.caseMappingAtomic.Store(&caseMapping)
		c.mu.Lock()
		c.serverCapabilities.CaseMapping = caseMapping
		c.mu.Unlock()

	case param == "UTF8ONLY":
		// Ratified UTF8ONLY: the server accepts only UTF-8 content. We already
		// emit UTF-8 exclusively, so recording the token is sufficient to be
		// compliant — no outgoing-encoding change is required.
		c.mu.Lock()
		c.serverCapabilities.UTF8Only = true
		c.mu.Unlock()

	case strings.HasPrefix(param, "EXTBAN="):
		// Ratified extban (incl. account-extban): EXTBAN=<prefix>,<types>, e.g.
		// "$,ajrxc". The 'a' type with this prefix matches "$a" / "$a:account".
		if prefix, types, ok := parseExtban(param[len("EXTBAN="):]); ok {
			c.mu.Lock()
			c.serverCapabilities.ExtbanPrefix = prefix
			c.serverCapabilities.ExtbanTypes = types
			c.mu.Unlock()
		}

	case strings.HasPrefix(param, "BOT="):
		// Ratified bot mode: BOT=<letter> advertises the user mode that marks a
		// client as a bot. We record the server's letter (conventionally 'B', but
		// servers may use 'b') so both recognition and self-announce use it verbatim.
		letters := []rune(param[len("BOT="):])
		if len(letters) == 1 {
			c.mu.Lock()
			c.serverCapabilities.BotModeChar = letters[0]
			c.mu.Unlock()
		}

	case strings.HasPrefix(param, "CHANMODES="):
		chanModesValue := param[len("CHANMODES="):]
		a, b, cc, d := classifyChanModes(chanModesValue)
		c.mu.Lock()
		c.serverCapabilities.ChanModes = chanModesValue
		c.serverCapabilities.ChanModesA = a
		c.serverCapabilities.ChanModesB = b
		c.serverCapabilities.ChanModesC = cc
		c.serverCapabilities.ChanModesD = d
		c.mu.Unlock()
		logger.Log.Debug().
			Str("chanmodes", chanModesValue).
			Str("network", c.network.Name).
			Msg("Parsed CHANMODES from ISUPPORT")
	}

	// MONITOR=<limit> (also the valueless "MONITOR") announces presence-monitoring
	// support and the max targets we may track. Handled outside the switch because
	// parseMonitorLimit accepts both the valued and valueless forms.
	if limit, ok := parseMonitorLimit(param); ok {
		c.mu.Lock()
		c.supportsMonitor = true
		c.monitorLimit = limit
		c.mu.Unlock()
	}
}

// ExtbanInfo returns the advertised EXTBAN prefix as a string (e.g. "$", or empty
// when unadvertised) and the supported type letters sorted (e.g. "acjrx"). The
// frontend uses this to detect account-extban support (type 'a') and to render
// "$a:account" ban masks meaningfully.
func (c *IRCClient) ExtbanInfo() (prefix string, types string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.serverCapabilities == nil {
		return "", ""
	}
	if c.serverCapabilities.ExtbanPrefix != 0 {
		prefix = string(c.serverCapabilities.ExtbanPrefix)
	}
	return prefix, sortedRunes(c.serverCapabilities.ExtbanTypes)
}

// BotMode returns the server-advertised bot user-mode letter (from the BOT=
// ISUPPORT token) as a string, or "" when the server did not advertise one.
// Self-announce and WHO-flag recognition both key off this letter.
func (c *IRCClient) BotMode() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	if c.serverCapabilities == nil || c.serverCapabilities.BotModeChar == 0 {
		return ""
	}
	return string(c.serverCapabilities.BotModeChar)
}

func (c *IRCClient) parsePREFIX(prefixValue string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.serverCapabilities.PrefixString = prefixValue
	c.serverCapabilities.Prefix = make(map[rune]rune)

	// Find the opening parenthesis
	openParen := strings.IndexRune(prefixValue, '(')
	if openParen == -1 {
		logger.Log.Warn().
			Str("prefix", prefixValue).
			Msg("Invalid PREFIX format: missing opening parenthesis")
		return
	}

	// Find the closing parenthesis
	closeParen := strings.IndexRune(prefixValue[openParen:], ')')
	if closeParen == -1 {
		logger.Log.Warn().
			Str("prefix", prefixValue).
			Msg("Invalid PREFIX format: missing closing parenthesis")
		return
	}
	closeParen += openParen

	// Extract mode letters (between parentheses)
	modeLetters := prefixValue[openParen+1 : closeParen]

	// Extract prefix characters (after closing parenthesis)
	prefixChars := prefixValue[closeParen+1:]

	// Map prefix characters to mode letters
	// The order should match: first prefix char maps to first mode letter, etc.
	modeRunes := []rune(modeLetters)
	prefixRunes := []rune(prefixChars)

	for i := 0; i < len(modeRunes) && i < len(prefixRunes); i++ {
		c.serverCapabilities.Prefix[prefixRunes[i]] = modeRunes[i]
	}

	logger.Log.Debug().
		Str("prefix", prefixValue).
		Str("modes", modeLetters).
		Str("prefixes", prefixChars).
		Str("network", c.network.Name).
		Msg("Parsed PREFIX from ISUPPORT")
}

// GetServerCapabilities returns a copy of the server capabilities
func (c *IRCClient) GetServerCapabilities() *ServerCapabilities {
	c.mu.RLock()
	defer c.mu.RUnlock()

	// Return a copy to avoid race conditions
	cap := &ServerCapabilities{
		Prefix:       make(map[rune]rune),
		PrefixString: c.serverCapabilities.PrefixString,
		ChanModes:    c.serverCapabilities.ChanModes,
		ChanTypes:    c.serverCapabilities.ChanTypes,
		CaseMapping:  c.serverCapabilities.CaseMapping,
		UTF8Only:     c.serverCapabilities.UTF8Only,
		ExtbanPrefix: c.serverCapabilities.ExtbanPrefix,
		Software:     c.serverCapabilities.Software,
	}

	// Copy the prefix map
	for k, v := range c.serverCapabilities.Prefix {
		cap.Prefix[k] = v
	}

	// Copy the extban type set
	if c.serverCapabilities.ExtbanTypes != nil {
		cap.ExtbanTypes = make(map[rune]bool, len(c.serverCapabilities.ExtbanTypes))
		for k, v := range c.serverCapabilities.ExtbanTypes {
			cap.ExtbanTypes[k] = v
		}
	}

	return cap
}

// GetChanModeClasses returns the resolved CHANMODES classes as sorted letter strings
// (A=list, B=param, C=set-param, D=flag), applying RFC1459 defaults when the server
// never advertised CHANMODES. Used to drive the capability-aware mode editor.
func (c *IRCClient) GetChanModeClasses() (a, b, cc, d string) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	cls := c.serverCapabilities.classification()
	return sortedRunes(cls.List), sortedRunes(cls.Param), sortedRunes(cls.SetParam), sortedRunes(cls.Flag)
}

// RequestChannelBans asks the server for the channel ban list (MODE #channel +b).
// Results arrive asynchronously via the EventChannelBanList event after RPL_ENDOFBANLIST.
func (c *IRCClient) RequestChannelBans(channel string) error {
	return c.SendRawCommand(fmt.Sprintf("MODE %s +b", channel))
}

// handleCTCPRequest handles incoming CTCP requests and sends appropriate responses
func (c *IRCClient) handleCTCPRequest(from, command, args string) {
	c.mu.RLock()
	connected := c.connected
	c.mu.RUnlock()

	if !connected {
		return
	}

	var response string
	switch command {
	case "VERSION":
		response = "Cascade IRC Client v1.0.0"
	case "TIME":
		response = time.Now().Format(time.RFC1123Z)
	case "PING":
		// Echo back the ping argument or use current timestamp
		if args != "" {
			response = args
		} else {
			response = fmt.Sprintf("%d", time.Now().Unix())
		}
	case "CLIENTINFO":
		response = "ACTION CLIENTINFO PING TIME VERSION"
	default:
		// Unknown CTCP command - don't respond
		return
	}

	// Send CTCP response as NOTICE with \001 delimiters
	ctcpResponse := fmt.Sprintf("\001%s %s\001", command, response)
	c.conn.Notice(from, ctcpResponse)

	// Log the CTCP request
	logger.Log.Debug().
		Str("from", from).
		Str("command", command).
		Str("response", response).
		Msg("Handled CTCP request")
}

// SendCTCPRequest sends a CTCP request to a target
func (c *IRCClient) SendCTCPRequest(target, command, args string) error {
	c.mu.RLock()
	if !c.connected {
		c.mu.RUnlock()
		return fmt.Errorf("not connected")
	}
	c.mu.RUnlock()

	var ctcpMessage string
	if args != "" {
		ctcpMessage = fmt.Sprintf("\001%s %s\001", command, args)
	} else {
		ctcpMessage = fmt.Sprintf("\001%s\001", command)
	}

	if err := c.conn.Privmsg(target, ctcpMessage); err != nil {
		return fmt.Errorf("failed to send CTCP request: %w", err)
	}

	// Store CTCP request in status window
	statusMsg := storage.Message{
		NetworkID:   c.networkID,
		ChannelID:   nil,
		User:        c.network.Nickname,
		Message:     fmt.Sprintf("CTCP %s sent to %s", command, target),
		MessageType: "ctcp",
		Timestamp:   time.Now(),
		RawLine:     "",
	}
	c.storage.WriteMessage(statusMsg)

	return nil
}

// handleNotice routes an inbound NOTICE. Extracted from the connection
// callback so the routing can be exercised end-to-end in tests by parsing a
// raw IRC line and calling it directly. Channel notices go to the channel
// buffer; notices from a user or service (a nick source) go to that sender's
// query pane keyed by pm_target; genuine server notices stay in Status.
func (c *IRCClient) handleNotice(e ircmsg.Message) {
	if len(e.Params) < 2 {
		return
	}
	target := e.Params[0]
	notice := e.Params[1]
	user := e.Nick()

	// IRCv3 bot mode: recognize a bot sender from the valueless `bot` tag.
	c.maybeMarkBotFromTag(e)
	// account-tag: learn the sender's account from the `@account` tag.
	c.maybeApplyAccountTag(e)

	// Check if this is a CTCP response (wrapped in \001)
	if len(notice) >= 2 && notice[0] == '\001' && notice[len(notice)-1] == '\001' {
		ctcpMessage := notice[1 : len(notice)-1] // Remove \001 delimiters
		parts := strings.Fields(ctcpMessage)
		if len(parts) > 0 {
			ctcpCommand := strings.ToUpper(parts[0])
			ctcpResponse := ""
			if len(parts) > 1 {
				ctcpResponse = strings.Join(parts[1:], " ")
			}

			// Store CTCP response in status window
			rawLine, _ := e.Line()
			statusMsg := storage.Message{
				NetworkID:   c.networkID,
				ChannelID:   nil, // Status window
				User:        user,
				Message:     fmt.Sprintf("CTCP %s reply from %s: %s", ctcpCommand, user, ctcpResponse),
				MessageType: "ctcp",
				Timestamp:   c.getMessageTime(e),
				RawLine:     rawLine,
			}
			c.storage.WriteMessage(statusMsg)
			return
		}
	}

	// Regular NOTICE.
	// Channel-targeted notices (e.g. bot/announcement notices) belong in that
	// channel's buffer, mirroring how channel PRIVMSGs are routed.
	if len(target) > 0 && (target[0] == '#' || target[0] == '&') {
		rawLine, _ := e.Line()
		var channelID *int64
		if ch, err := c.storage.GetChannelByName(c.networkID, target); err == nil {
			channelID = &ch.ID
		} else {
			logger.Log.Debug().Err(err).Str("channel", target).Msg("Channel not found for notice")
		}
		c.storage.WriteMessageSync(storage.Message{
			NetworkID:      c.networkID,
			ChannelID:      channelID,
			User:           user,
			Message:        notice,
			MessageType:    "notice",
			Timestamp:      c.getMessageTime(e),
			RawLine:        rawLine,
			MsgID:          c.getMsgID(e),
			ReplyMsgID:     c.getReplyTag(e),
			ChannelContext: c.getChannelContext(e),
		})
		c.eventBus.Emit(events.Event{
			Type: EventMessageReceived,
			Data: map[string]interface{}{
				"network":     c.network.Address,
				"networkId":   c.networkID,
				"channel":     target,
				"user":        user,
				"message":     notice,
				"messageType": "notice",
				"networkName": c.network.Name,
				"account":     c.accountFor(user),
				"msgid":       c.getMsgID(e),
				"messageUnix": c.getMessageTime(e).Unix(),
			},
			Timestamp: time.Now(),
			Source:    events.EventSourceIRC,
		})
		return
	}

	// Non-channel NOTICE.
	// Notices from a user/service (ChanServ, NickServ, another nick) carry a
	// "nick!user@host" prefix and route to that sender's query pane, keyed by
	// pm_target exactly like a PRIVMSG PM. Notices with a bare server-name
	// prefix (or addressed to "*") are genuine server notices and stay in the
	// Status window.
	if c.isMe(target) || (len(target) > 0 && target[0] != '#' && target[0] != '&') {
		rawLine, _ := e.Line()
		pmTarget := c.noticePMTarget(e.Source, user, target)

		if pmTarget != "" {
			// Open/refresh the query conversation so the pane appears in the sidebar.
			if _, err := c.storage.GetOrCreatePMConversation(c.networkID, pmTarget, c.network.Nickname); err != nil {
				logger.Log.Error().Err(err).Str("user", user).Str("pmTarget", pmTarget).Msg("Failed to create/get PM conversation for notice")
			}
		}

		msg := storage.Message{
			NetworkID:      c.networkID,
			ChannelID:      nil, // PM rows and status rows both have a nil channel
			User:           user,
			Message:        notice,
			MessageType:    "notice",
			Timestamp:      c.getMessageTime(e),
			RawLine:        rawLine,
			PMTarget:       pmTarget, // "" keeps it in Status; non-empty routes to a query pane
			MsgID:          c.getMsgID(e),
			ReplyMsgID:     c.getReplyTag(e),
			ChannelContext: c.getChannelContext(e),
		}

		if pmTarget == "" {
			// Server notice — buffered write into Status, no event (unchanged).
			c.storage.WriteMessage(msg)
		} else {
			// Service/user notice — sync write so it shows immediately, plus a
			// message event so the query pane badges and updates live. The
			// messageType marks it a notice so chatty services don't trigger a
			// desktop notification on every line.
			c.storage.WriteMessageSync(msg)
			c.eventBus.Emit(events.Event{
				Type: EventMessageReceived,
				Data: map[string]interface{}{
					"network":     c.network.Address,
					"networkId":   c.networkID,
					"channel":     target,
					"user":        user,
					"message":     notice,
					"messageType": "notice",
					"networkName": c.network.Name,
					"account":     c.accountFor(user),
					"msgid":       c.getMsgID(e),
					"messageUnix": c.getMessageTime(e).Unix(),
				},
				Timestamp: time.Now(),
				Source:    events.EventSourceIRC,
			})
		}
	}
}
