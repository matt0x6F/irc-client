package irc

import (
	"regexp"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/storage"
)

// ActivitySource identifies which registered source produced an inbox item.
type ActivitySource string

const (
	ActivityHighlight ActivitySource = "highlight"
	ActivityKeyword   ActivitySource = "keyword"
	ActivityInvite    ActivitySource = "invite"
	ActivityPM        ActivitySource = "pm"
)

const activityPreviewMax = 140

// ActivityConfig is the user's enabled-sources + keyword configuration.
type ActivityConfig struct {
	Highlights  bool
	Keywords    bool
	Invites     bool
	PMs         bool
	Notices     bool // include NOTICE-type messages in Activity
	Privmsgs    bool // include PRIVMSG-type messages in Activity
	KeywordList []string
}

// IsChannelName reports whether target names a channel (vs a PM peer).
func IsChannelName(s string) bool {
	return strings.HasPrefix(s, "#") || strings.HasPrefix(s, "&")
}

// matchHighlight reports whether text mentions word on a word boundary,
// case-insensitively. Ported from the frontend isMention regex.
func matchHighlight(text, word string) bool {
	if word == "" {
		return false
	}
	pattern := `(?i)(?:^|[^a-zA-Z0-9_])` + regexp.QuoteMeta(word) + `(?:[^a-zA-Z0-9_]|$)`
	return regexp.MustCompile(pattern).MatchString(text)
}

// matchKeyword returns the first keyword that appears in text (word-boundary,
// case-insensitive), or ("", false).
func matchKeyword(text string, keywords []string) (string, bool) {
	for _, kw := range keywords {
		if kw == "" {
			continue
		}
		if matchHighlight(text, kw) {
			return strings.ToLower(kw), true
		}
	}
	return "", false
}

// ClassifyMessageActivity decides which activity (if any) an inbound message
// produces. channel is "" for PMs; isPM marks a direct message. messageType is
// the IRC message type ("privmsg"/"notice"); the per-type toggles veto it here.
func ClassifyMessageActivity(cfg ActivityConfig, currentNick, channel, sender, text, messageType string, isPM bool) (ActivitySource, string, bool) {
	if messageType == "notice" && !cfg.Notices {
		return "", "", false
	}
	if messageType == "privmsg" && !cfg.Privmsgs {
		return "", "", false
	}
	if isPM {
		if cfg.PMs {
			return ActivityPM, "", true
		}
		return "", "", false
	}
	if cfg.Highlights && matchHighlight(text, currentNick) {
		return ActivityHighlight, "", true
	}
	if cfg.Keywords {
		if kw, ok := matchKeyword(text, cfg.KeywordList); ok {
			return ActivityKeyword, kw, true
		}
	}
	return "", "", false
}

// ActivityItemFromMessage maps a classified message into a storage row.
func ActivityItemFromMessage(networkID int64, src ActivitySource, keyword, channel, sender, message, msgid string, isPM bool, ts time.Time) storage.ActivityItem {
	target := channel
	if isPM {
		target = sender
	}
	return storage.ActivityItem{
		NetworkID:  networkID,
		SourceType: string(src),
		Target:     target,
		Actor:      sender,
		Preview:    truncatePreview(message),
		MsgID:      msgid,
		Keyword:    keyword,
		Seen:       false,
		Timestamp:  ts,
	}
}

func truncatePreview(s string) string {
	r := []rune(s)
	if len(r) <= activityPreviewMax {
		return s
	}
	return string(r[:activityPreviewMax]) + "…"
}
