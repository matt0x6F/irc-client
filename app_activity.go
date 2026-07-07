package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/matt0x6f/irc-client/internal/events"
	"github.com/matt0x6f/irc-client/internal/irc"
	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
)

const activitySettingsKey = "activity.settings"

// ActivitySettings is the user's activity-source configuration (Wails-exported).
type ActivitySettings struct {
	Highlights  bool     `json:"highlights"`
	Keywords    bool     `json:"keywords"`
	Invites     bool     `json:"invites"`
	PMs         bool     `json:"pms"`
	Notices     bool     `json:"notices"`
	Privmsgs    bool     `json:"privmsgs"`
	KeywordList []string `json:"keywordList"`
}

func defaultActivitySettings() ActivitySettings {
	return ActivitySettings{Highlights: true, Keywords: true, Invites: true, PMs: true, Notices: true, Privmsgs: true, KeywordList: []string{}}
}

func (s ActivitySettings) toConfig() irc.ActivityConfig {
	return irc.ActivityConfig{
		Highlights:  s.Highlights,
		Keywords:    s.Keywords,
		Invites:     s.Invites,
		PMs:         s.PMs,
		Notices:     s.Notices,
		Privmsgs:    s.Privmsgs,
		KeywordList: s.KeywordList,
	}
}

// GetActivitySettings returns the stored settings, or defaults when unset.
func (a *App) GetActivitySettings() (ActivitySettings, error) {
	raw, err := a.storage.GetSetting(activitySettingsKey)
	if err != nil || raw == "" {
		return defaultActivitySettings(), nil
	}
	s := defaultActivitySettings()
	if err := json.Unmarshal([]byte(raw), &s); err != nil {
		return defaultActivitySettings(), fmt.Errorf("decode activity settings: %w", err)
	}
	if s.KeywordList == nil {
		s.KeywordList = []string{}
	}
	return s, nil
}

// SetActivitySettings persists the settings as JSON.
func (a *App) SetActivitySettings(s ActivitySettings) error {
	if s.KeywordList == nil {
		s.KeywordList = []string{}
	}
	raw, err := json.Marshal(s)
	if err != nil {
		return fmt.Errorf("encode activity settings: %w", err)
	}
	if err := a.storage.SetSetting(activitySettingsKey, string(raw)); err != nil {
		return fmt.Errorf("persist activity settings: %w", err)
	}
	return nil
}

// recordMessageActivity classifies one inbound message and, on a match, writes
// an activity row and signals the frontend.
func (a *App) recordMessageActivity(cfg irc.ActivityConfig, currentNick string, networkID int64, channel, sender, message, msgid, messageType string, isPM bool, ts time.Time) {
	if currentNick != "" && strings.EqualFold(sender, currentNick) {
		return
	}
	if ignored, err := a.storage.IsSenderIgnored(networkID, sender); err == nil && ignored {
		return
	}
	src, keyword, ok := irc.ClassifyMessageActivity(cfg, currentNick, channel, sender, message, messageType, isPM)
	if !ok {
		return
	}
	item := irc.ActivityItemFromMessage(networkID, src, keyword, channel, sender, message, msgid, isPM, ts)
	if _, err := a.storage.WriteActivityItem(item); err != nil {
		logger.Log.Warn().Err(err).Msg("Failed to write activity item")
		return
	}
	a.emit("activity-changed")
}

// dispatchMessageActivity extracts message.received event data and records activity.
func (a *App) dispatchMessageActivity(event events.Event) {
	settings, err := a.GetActivitySettings()
	if err != nil {
		return
	}
	networkID, ok := a.resolveNetworkID(event.Data)
	if !ok {
		return
	}
	channel, _ := event.Data["channel"].(string)
	sender, _ := event.Data["user"].(string)
	message, _ := event.Data["message"].(string)
	msgid, _ := event.Data["msgid"].(string)
	messageType, _ := event.Data["messageType"].(string)
	// message.received sets Data["channel"] to e.Params[0]: a channel name for
	// channel messages, our own nick for PMs. Derive PM-vs-channel from that.
	isPM := channel != "" && !irc.IsChannelName(channel)

	a.mu.RLock()
	client := a.ircClients[networkID]
	a.mu.RUnlock()
	if client == nil {
		// Cannot establish identity without a live client — do not guess.
		return
	}
	currentNick := client.CurrentNick()
	a.recordMessageActivity(settings.toConfig(), currentNick, networkID, channel, sender, message, msgid, messageType, isPM, event.Timestamp)
}

const activityItemsLimit = 500

// GetActivityItems returns the inbox rows, newest first.
func (a *App) GetActivityItems() ([]storage.ActivityItem, error) {
	items, err := a.storage.ListActivityItems(activityItemsLimit)
	if err != nil {
		return nil, err
	}
	if items == nil {
		items = []storage.ActivityItem{}
	}
	return items, nil
}

func (a *App) MarkActivitySeen(id int64) error { return a.storage.MarkActivitySeen(id) }
func (a *App) MarkAllActivitySeen() error      { return a.storage.MarkAllActivitySeen() }
func (a *App) DismissActivity(id int64) error  { return a.storage.DismissActivity(id) }
func (a *App) ClearSeenActivity() error        { return a.storage.ClearSeenActivity() }
func (a *App) ClearAllActivity() error         { return a.storage.ClearAllActivity() }

// GetMessageIDByMsgID resolves an IRCv3 msgid to its numeric message id (0 if not found).
func (a *App) GetMessageIDByMsgID(networkID int64, msgid string) (int64, error) {
	return a.storage.MessageIDByMsgID(networkID, msgid)
}
