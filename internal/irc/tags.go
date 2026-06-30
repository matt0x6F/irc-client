package irc

import "github.com/ergochat/irc-go/ircmsg"

const (
	tagReplyDraft          = "+draft/reply"
	tagReply               = "+reply"
	tagChannelContextDraft = "+draft/channel-context"
	tagChannelContext      = "+channel-context"
)

// buildSendTags builds the client-only tag map for an outbound PRIVMSG. Emits
// the +draft/ forms (widest client support today). Returns nil when no tags
// apply so callers can fall back to the untagged send path.
func buildSendTags(replyMsgID, channelContext string) map[string]string {
	if replyMsgID == "" && channelContext == "" {
		return nil
	}
	tags := make(map[string]string, 2)
	if replyMsgID != "" {
		tags[tagReplyDraft] = replyMsgID
	}
	if channelContext != "" {
		tags[tagChannelContextDraft] = channelContext
	}
	return tags
}

// firstTag returns the value of the first present tag among names.
func firstTag(e ircmsg.Message, names ...string) string {
	for _, n := range names {
		if present, v := e.GetTag(n); present && v != "" {
			return v
		}
	}
	return ""
}

// getReplyTag returns the parent msgid referenced by +draft/reply (or the bare
// +reply form), or "" if absent.
func (c *IRCClient) getReplyTag(e ircmsg.Message) string {
	return firstTag(e, tagReplyDraft, tagReply)
}

// getChannelContext returns the channel a private message is about, from
// +draft/channel-context (or bare +channel-context). Per spec the value MUST be
// a valid channel name; anything else is ignored.
func (c *IRCClient) getChannelContext(e ircmsg.Message) string {
	v := firstTag(e, tagChannelContextDraft, tagChannelContext)
	if v == "" || !c.isChannelName(v) {
		return ""
	}
	return v
}
