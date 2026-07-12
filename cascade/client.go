package cascade

// Client is the script's proactive handle, passed to Setup(c). Its callbacks are
// bound by the host; scripts never construct it.
type Client struct {
	sayFn        func(networkName, target, message string)
	everyFn      func(interval string, fn func())
	afterFn      func(delay string, fn func())
	connectedFn  func(networkName string) bool
	nickFn       func(networkName string) string
	isMeFn       func(networkName, nick string) bool
	userStatusFn func(networkName, nick string) UserStatus
	noticeFn     func(networkName, target, message string)
	actionFn     func(networkName, target, message string)
	joinFn       func(networkName, channel, key string)
	partFn       func(networkName, channel, reason string)
	changeNickFn func(networkName, nick string)
	setAwayFn    func(networkName, message string)
}

// WithIRCActions binds the proactive IRC operations available to scripts.
func WithIRCActions(
	notice func(networkName, target, message string),
	action func(networkName, target, message string),
	join func(networkName, channel, key string),
	part func(networkName, channel, reason string),
	changeNick func(networkName, nick string),
	setAway func(networkName, message string),
) ClientOption {
	return func(c *Client) {
		c.noticeFn = notice
		c.actionFn = action
		c.joinFn = join
		c.partFn = part
		c.changeNickFn = changeNick
		c.setAwayFn = setAway
	}
}

// ClientOption binds an optional host capability to a Client.
type ClientOption func(*Client)

// WithNetworkQueries binds read-only network and user state lookups.
func WithNetworkQueries(
	connected func(networkName string) bool,
	nick func(networkName string) string,
	isMe func(networkName, nick string) bool,
	userStatus func(networkName, nick string) UserStatus,
) ClientOption {
	return func(c *Client) {
		c.connectedFn = connected
		c.nickFn = nick
		c.isMeFn = isMe
		c.userStatusFn = userStatus
	}
}

// NewClient is the host-side constructor.
func NewClient(say func(networkName, target, message string), every func(interval string, fn func()), after func(delay string, fn func()), options ...ClientOption) *Client {
	c := &Client{sayFn: say, everyFn: every, afterFn: after}
	for _, option := range options {
		if option != nil {
			option(c)
		}
	}
	return c
}

// Network returns a handle to a configured network by name. Actions resolve the
// network at call time, so a handle to a disconnected/unknown network simply
// no-ops (the host logs it).
func (c *Client) Network(name string) Network {
	return Network{
		name:         name,
		sayFn:        c.sayFn,
		connectedFn:  c.connectedFn,
		nickFn:       c.nickFn,
		isMeFn:       c.isMeFn,
		userStatusFn: c.userStatusFn,
		noticeFn:     c.noticeFn,
		actionFn:     c.actionFn,
		joinFn:       c.joinFn,
		partFn:       c.partFn,
		changeNickFn: c.changeNickFn,
		setAwayFn:    c.setAwayFn,
	}
}

// Every schedules fn to run repeatedly on the given interval (e.g. "5m", "30s").
func (c *Client) Every(interval string, fn func()) {
	if c.everyFn != nil {
		c.everyFn(interval, fn)
	}
}

// After schedules fn to run once after the given delay.
func (c *Client) After(delay string, fn func()) {
	if c.afterFn != nil {
		c.afterFn(delay, fn)
	}
}

// Network is a handle to one configured network.
type Network struct {
	name         string
	sayFn        func(networkName, target, message string)
	connectedFn  func(networkName string) bool
	nickFn       func(networkName string) string
	isMeFn       func(networkName, nick string) bool
	userStatusFn func(networkName, nick string) UserStatus
	noticeFn     func(networkName, target, message string)
	actionFn     func(networkName, target, message string)
	joinFn       func(networkName, channel, key string)
	partFn       func(networkName, channel, reason string)
	changeNickFn func(networkName, nick string)
	setAwayFn    func(networkName, message string)
}

// UserStatus is Cascade's latest session-local metadata snapshot for a nick.
// Known distinguishes a real all-zero snapshot from a nick the host has not seen.
type UserStatus struct {
	Known       bool
	Away        bool
	AwayMessage string
	Account     string
	Host        string
	Realname    string
}

// User is a network-scoped nickname handle.
type User struct {
	networkName string
	nick        string
	sayFn       func(networkName, target, message string)
	isMeFn      func(networkName, nick string) bool
	statusFn    func(networkName, nick string) UserStatus
	noticeFn    func(networkName, target, message string)
}

// Name returns the configured network name used by this handle.
func (n Network) Name() string { return n.name }

// IsConnected reports the host's current connection state for this network.
func (n Network) IsConnected() bool {
	return n.connectedFn != nil && n.connectedFn(n.name)
}

// Nick returns our current nickname on this network.
func (n Network) Nick() string {
	if n.nickFn == nil {
		return ""
	}
	return n.nickFn(n.name)
}

// IsMe compares nick with our current nick using the host's IRC case mapping.
func (n Network) IsMe(nick string) bool {
	return n.isMeFn != nil && n.isMeFn(n.name, nick)
}

// Self returns a User handle for our current nickname.
func (n Network) Self() User { return n.User(n.Nick()) }

// User returns a handle for nick without performing network I/O.
func (n Network) User(nick string) User {
	return User{networkName: n.name, nick: nick, sayFn: n.sayFn, noticeFn: n.noticeFn, isMeFn: n.isMeFn, statusFn: n.userStatusFn}
}

// Nick returns the nickname addressed by this handle.
func (u User) Nick() string { return u.nick }

// IsSelf reports whether this handle addresses our current nick.
func (u User) IsSelf() bool {
	return u.isMeFn != nil && u.isMeFn(u.networkName, u.nick)
}

// Status returns the latest host snapshot, or an unknown zero value.
func (u User) Status() UserStatus {
	if u.statusFn == nil {
		return UserStatus{}
	}
	return u.statusFn(u.networkName, u.nick)
}

// Known reports whether Cascade has current-session metadata for this user.
func (u User) Known() bool { return u.Status().Known }

// IsAway reports the latest known away flag. Check Known when unknown matters.
func (u User) IsAway() bool { return u.Status().Away }

// Say sends a PRIVMSG to this user. Fire-and-forget.
func (u User) Say(message string) {
	if u.sayFn != nil {
		u.sayFn(u.networkName, u.nick, message)
	}
}

// Notice sends a NOTICE to this user. Fire-and-forget.
func (u User) Notice(message string) {
	if u.noticeFn != nil {
		u.noticeFn(u.networkName, u.nick, message)
	}
}

// Say sends a PRIVMSG to target on this network. Fire-and-forget.
func (n Network) Say(target, message string) {
	if n.sayFn != nil {
		n.sayFn(n.name, target, message)
	}
}

// Notice sends a NOTICE to target. Fire-and-forget.
func (n Network) Notice(target, message string) {
	if n.noticeFn != nil {
		n.noticeFn(n.name, target, message)
	}
}

// Action sends a CTCP ACTION to target. Fire-and-forget.
func (n Network) Action(target, message string) {
	if n.actionFn != nil {
		n.actionFn(n.name, target, message)
	}
}

// Join joins channel without a key. Fire-and-forget.
func (n Network) Join(channel string) { n.JoinWithKey(channel, "") }

// JoinWithKey joins channel with an optional key. Fire-and-forget.
func (n Network) JoinWithKey(channel, key string) {
	if n.joinFn != nil {
		n.joinFn(n.name, channel, key)
	}
}

// Part leaves channel with an optional reason. Fire-and-forget.
func (n Network) Part(channel, reason string) {
	if n.partFn != nil {
		n.partFn(n.name, channel, reason)
	}
}

// ChangeNick requests a new nickname. Fire-and-forget.
func (n Network) ChangeNick(nick string) {
	if n.changeNickFn != nil {
		n.changeNickFn(n.name, nick)
	}
}

// SetAway requests away state with message. Fire-and-forget.
func (n Network) SetAway(message string) {
	if n.setAwayFn != nil {
		n.setAwayFn(n.name, message)
	}
}

// ClearAway requests present state. Fire-and-forget.
func (n Network) ClearAway() { n.SetAway("") }
