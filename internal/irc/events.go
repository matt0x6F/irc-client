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
)

