package irc

// Event types emitted by the IRC client
const (
	EventMessageReceived    = "message.received"
	EventMessageSent        = "message.sent"
	EventUserJoined         = "user.joined"
	EventUserParted         = "user.parted"
	EventUserQuit           = "user.quit"
	EventUserNick           = "user.nick"
	EventChannelTopic       = "channel.topic"
	EventChannelMode        = "channel.mode"
	EventConnectionEstablished = "connection.established"
	EventConnectionLost     = "connection.lost"
	EventError              = "error"
	EventSASLStarted        = "sasl.started"
	EventSASLSuccess        = "sasl.success"
	EventSASLFailed         = "sasl.failed"
	EventSASLAborted        = "sasl.aborted"
)

