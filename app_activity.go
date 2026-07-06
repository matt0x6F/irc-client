package main

import (
	"encoding/json"
	"fmt"

	"github.com/matt0x6f/irc-client/internal/irc"
)

const activitySettingsKey = "activity.settings"

// ActivitySettings is the user's activity-source configuration (Wails-exported).
type ActivitySettings struct {
	Highlights  bool     `json:"highlights"`
	Keywords    bool     `json:"keywords"`
	Invites     bool     `json:"invites"`
	PMs         bool     `json:"pms"`
	KeywordList []string `json:"keywordList"`
}

func defaultActivitySettings() ActivitySettings {
	return ActivitySettings{Highlights: true, Keywords: true, Invites: true, PMs: true, KeywordList: []string{}}
}

func (s ActivitySettings) toConfig() irc.ActivityConfig {
	return irc.ActivityConfig{
		Highlights:  s.Highlights,
		Keywords:    s.Keywords,
		Invites:     s.Invites,
		PMs:         s.PMs,
		KeywordList: s.KeywordList,
	}
}

// GetActivitySettings returns the stored settings, or defaults when unset.
func (a *App) GetActivitySettings() (ActivitySettings, error) {
	raw, err := a.storage.GetSetting(activitySettingsKey)
	if err != nil || raw == "" {
		return defaultActivitySettings(), nil
	}
	var s ActivitySettings
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
