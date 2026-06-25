package irc

// Event types emitted by the IRC client
const (
	EventMessageReceived       = "message.received"
	EventMessageSent           = "message.sent"
	EventUserJoined            = "user.joined"
	EventUserParted            = "user.parted"
	EventUserQuit              = "user.quit"
	EventUserKicked            = "user.kicked"
	EventUserNick              = "user.nick"
	EventNickChanged           = "nick.changed" // our own nick changed (distinct from EventUserNick, which is anyone)
	EventChannelTopic          = "channel.topic"
	EventChannelMode           = "channel.mode"
	EventChannelUserMode       = "channel.usermode"
	EventChannelBanList        = "channel.banlist"
	EventChannelsChanged       = "channels.changed"
	EventConnectionEstablished = "connection.established"
	EventConnectionLost        = "connection.lost"
	EventError                 = "error"
	EventSASLStarted           = "sasl.started"
	EventSASLSuccess           = "sasl.success"
	EventSASLFailed            = "sasl.failed"
	EventSASLAborted           = "sasl.aborted"
	EventWhoisReceived         = "whois.received"
	EventChannelListItem       = "channel.list.item"
	EventChannelListEnd        = "channel.list.end"
	EventHistoryReceived       = "history.received"
	EventBotDetected           = "bot.detected"    // a nick was recognized as an IRCv3 bot (bot tag or RPL_WHOISBOT)
	EventUserMetaChanged       = "user.meta"       // a user's live roster attributes changed (away/account/host)
	EventSTSPolicy             = "sts.policy"      // server advertised an IRCv3 STS policy in CAP LS
	EventMonitorChanged        = "monitor.changed" // a monitored nick's online/offline state changed (MONITOR)
	EventTypingReceived        = "typing.received" // a peer sent an IRCv3 +typing client tag (active/paused/done)
)

// UserMeta holds the live, session-local roster attributes Cascade tracks for a
// nick via the IRCv3 caps away-notify, account-notify, extended-join, chghost,
// and account-tag. It is deliberately not persisted: a nick's away/account/host
// is only meaningful for the current session and is rebuilt on reconnect.
type UserMeta struct {
	Away        bool   `json:"away"`         // true while the user is marked away (away-notify)
	AwayMessage string `json:"away_message"` // the away reason, if any
	Account     string `json:"account"`      // account the user is logged in as; "" when not logged in
	Host        string `json:"host"`         // user@host learned from chghost / userhost-in-names; "" until seen
	Realname    string `json:"realname"`     // realname learned from setname / extended-join; "" until seen
}

// BanEntry represents a single entry from a channel ban list (RPL_BANLIST 367)
type BanEntry struct {
	Mask string `json:"mask"`
	By   string `json:"by"`
	Time int64  `json:"time"` // unix timestamp the ban was set
}

// ChannelListItem represents a single entry from the LIST response
type ChannelListItem struct {
	Channel   string `json:"channel"`
	Users     int    `json:"users"`
	Topic     string `json:"topic"`
	NetworkID int64  `json:"networkId"`
}

// WhoisInfo represents parsed WHOIS information
type WhoisInfo struct {
	Nickname    string   `json:"nickname"`
	Username    string   `json:"username"`
	Hostmask    string   `json:"hostmask"`
	RealName    string   `json:"real_name"`
	Server      string   `json:"server"`
	ServerInfo  string   `json:"server_info"`
	Channels    []string `json:"channels"`
	IdleTime    int64    `json:"idle_time"`    // seconds
	SignOnTime  int64    `json:"sign_on_time"` // unix timestamp
	AccountName string   `json:"account_name"` // if logged in
	Network     string   `json:"network"`
	IsBot       bool     `json:"is_bot"` // true if the server returned RPL_WHOISBOT (335)
}
