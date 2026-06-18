package main

import (
	"path/filepath"
	"strings"

	"github.com/matt0x6f/irc-client/internal/logger"
	"github.com/matt0x6f/irc-client/internal/storage"
)

// Setting keys for file-based logging. Persisted in the SQLite settings table
// so they survive restarts (WKWebView localStorage does not).
const (
	settingLogFileEnabled = "log.file.enabled"
	settingLogFilePath    = "log.file.path"
	settingLogLevel       = "log.level"
)

// LogConfig is the file-logging configuration exposed to the frontend.
type LogConfig struct {
	Enabled bool   `json:"enabled"`
	Path    string `json:"path"`
	Level   string `json:"level"` // "debug" | "info" | "warn" | "error"
}

// defaultLogPath returns the log file location used when the user hasn't set one.
// It sits alongside the database under the data directory.
func defaultLogPath(dataDir string) string {
	return filepath.Join(dataDir, "logs", "cascade-chat.log")
}

// normalizeLevel lowercases and validates a level string, defaulting to "info".
func normalizeLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case "debug":
		return "debug"
	case "warn", "warning":
		return "warn"
	case "error":
		return "error"
	default:
		return "info"
	}
}

// readLogConfig assembles the current LogConfig from persisted settings, filling
// defaults for any missing or blank values. Read errors are treated as "unset"
// so the caller always gets a usable config.
func readLogConfig(stor *storage.Storage, dataDir string) LogConfig {
	get := func(key string) string {
		v, err := stor.GetSetting(key)
		if err != nil {
			return ""
		}
		return strings.TrimSpace(v)
	}

	path := get(settingLogFilePath)
	if path == "" {
		path = defaultLogPath(dataDir)
	}

	return LogConfig{
		Enabled: get(settingLogFileEnabled) == "true",
		Path:    path,
		Level:   normalizeLevel(get(settingLogLevel)),
	}
}

// applyLogConfig reads persisted logging preferences and applies them to the
// global logger. A failure to open the log file must never block startup, so
// errors only downgrade to stderr-only logging with a warning.
func applyLogConfig(stor *storage.Storage, dataDir string) {
	cfg := readLogConfig(stor, dataDir)
	if err := logger.Configure(logger.Options{
		FileEnabled: cfg.Enabled,
		FilePath:    cfg.Path,
		Level:       cfg.Level,
	}); err != nil {
		logger.Log.Warn().Err(err).Str("path", cfg.Path).Msg("File logging unavailable; using stderr only")
	}
}

// GetLogConfig returns the current file-logging configuration, with defaults
// applied for any unset values.
func (a *App) GetLogConfig() (LogConfig, error) {
	return readLogConfig(a.storage, a.dataDir), nil
}

// SetLogConfig validates, applies, and persists a new file-logging
// configuration. The new config is applied to the logger first so a bad path is
// rejected before anything is written — on error the previous configuration and
// stored settings are left untouched.
func (a *App) SetLogConfig(enabled bool, path string, level string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		path = defaultLogPath(a.dataDir)
	}
	level = normalizeLevel(level)

	// Apply first — Configure prepares the directory and validates the path,
	// returning an error without disturbing current state if it can't.
	if err := logger.Configure(logger.Options{
		FileEnabled: enabled,
		FilePath:    path,
		Level:       level,
	}); err != nil {
		return err
	}

	enabledStr := "false"
	if enabled {
		enabledStr = "true"
	}
	if err := a.storage.SetSetting(settingLogFileEnabled, enabledStr); err != nil {
		return err
	}
	if err := a.storage.SetSetting(settingLogFilePath, path); err != nil {
		return err
	}
	if err := a.storage.SetSetting(settingLogLevel, level); err != nil {
		return err
	}

	// Let other open windows reconcile their copy of the config.
	a.emit("log:config-changed", LogConfig{Enabled: enabled, Path: path, Level: level})
	return nil
}

// GetDefaultLogPath returns the default log file location, for use as a UI
// placeholder when the user hasn't chosen a custom path.
func (a *App) GetDefaultLogPath() string {
	return defaultLogPath(a.dataDir)
}
