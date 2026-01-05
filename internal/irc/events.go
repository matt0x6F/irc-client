package irc

// Event types emitted by the IRC client
const (
	EventMessageReceived    = "message.received"
	EventMessageSent        = "message.sent"
	EventUserJoined         = "user.joined"
	EventUserParted         = "user.parted"
	EventUserQuit           = "user.quit"
	EventUserKicked         = "user.kicked"
	EventUserNick           = "user.nick"
	EventChannelTopic       = "channel.topic"
	EventChannelMode        = "channel.mode"
	EventChannelsChanged    = "channels.changed"
	EventConnectionEstablished = "connection.established"
	EventConnectionLost     = "connection.lost"
	EventError              = "error"
	EventSASLStarted        = "sasl.started"
	EventSASLSuccess        = "sasl.success"
	EventSASLFailed         = "sasl.failed"
	EventSASLAborted        = "sasl.aborted"
	EventWhoisReceived      = "whois.received"
)

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
}

