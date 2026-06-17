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
	EventBotDetected           = "bot.detected" // a nick was recognized as an IRCv3 bot (bot tag or RPL_WHOISBOT)
)

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
