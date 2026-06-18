package logger

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rs/zerolog"
	lumberjack "gopkg.in/natefinch/lumberjack.v2"
)

// Log is the process-wide logger. It is built ONCE in init() and never
// reassigned, so the many `logger.Log.X()` call sites stay race-free. What it
// writes to is swapped atomically by Configure via the sink below.
var Log zerolog.Logger

// sink is the single, stable writer Log always points at. Configure swaps its
// underlying target atomically; Log itself is immutable after init.
var sink = &swappableWriter{}

// File rotation defaults. Not surfaced in the UI — the user controls only the
// path, level, and on/off toggle (see Options).
const (
	logMaxSizeMB  = 10
	logMaxBackups = 3
	logCompress   = true
)

func init() {
	// Default before storage is available: human-readable, colored, stderr-only,
	// Info level. App.NewApp calls Configure once the DB is open to apply the
	// user's persisted preferences.
	sink.store(consoleWriter(os.Stderr, false))
	Log = zerolog.New(sink).With().Timestamp().Logger()
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

// Options describes the desired logging configuration. The zero value
// (FileEnabled false) means stderr-only, which is the startup default.
type Options struct {
	FileEnabled bool
	FilePath    string
	Level       string // "debug" | "info" | "warn" | "error"; unknown falls back to info
}

var (
	cfgMu      sync.Mutex
	curRotator *lumberjack.Logger // the file handle currently in use, if any
)

// Configure applies a logging configuration live. It rebuilds the writer set
// (stderr console always; plus a rotating file when enabled) and swaps it into
// the sink atomically, then updates the global level. Safe to call repeatedly,
// e.g. when the user changes the setting at runtime.
//
// Returns an error only when an enabled file path cannot be prepared; in that
// case the previous configuration is left untouched.
func Configure(opts Options) error {
	cfgMu.Lock()
	defer cfgMu.Unlock()

	w, rotator, err := buildSink(opts)
	if err != nil {
		return err
	}

	// Point the sink at the new writer BEFORE closing the old file, so no
	// in-flight write lands on a closed handle.
	sink.store(w)
	zerolog.SetGlobalLevel(ParseLevel(opts.Level))

	if curRotator != nil {
		_ = curRotator.Close()
	}
	curRotator = rotator
	return nil
}

// Close releases the log file, if one is open. Call on app shutdown to flush
// and close cleanly.
func Close() error {
	cfgMu.Lock()
	defer cfgMu.Unlock()
	if curRotator == nil {
		return nil
	}
	err := curRotator.Close()
	curRotator = nil
	return err
}

// SetLevel sets the global log level directly.
func SetLevel(level zerolog.Level) {
	zerolog.SetGlobalLevel(level)
}

// ParseLevel converts a level string to a zerolog.Level, defaulting to Info for
// empty or unrecognized input.
func ParseLevel(s string) zerolog.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "debug":
		return zerolog.DebugLevel
	case "warn", "warning":
		return zerolog.WarnLevel
	case "error":
		return zerolog.ErrorLevel
	default:
		return zerolog.InfoLevel
	}
}

// buildSink constructs the writer for the given options. The returned writer is
// wrapped in zerolog.SyncWriter so concurrent goroutines can log safely (the
// ConsoleWriter is not concurrency-safe on its own). The lumberjack handle is
// returned separately so Configure can close the previous one after swapping.
func buildSink(opts Options) (io.Writer, *lumberjack.Logger, error) {
	writers := []io.Writer{consoleWriter(os.Stderr, false)}

	var rotator *lumberjack.Logger
	if opts.FileEnabled {
		path := strings.TrimSpace(opts.FilePath)
		if path == "" {
			return nil, nil, fmt.Errorf("file logging enabled but no path provided")
		}
		path = expandHome(path)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			return nil, nil, fmt.Errorf("create log directory: %w", err)
		}
		rotator = &lumberjack.Logger{
			Filename:   path,
			MaxSize:    logMaxSizeMB,
			MaxBackups: logMaxBackups,
			Compress:   logCompress,
		}
		writers = append(writers, consoleWriter(rotator, true))
	}

	return zerolog.SyncWriter(zerolog.MultiLevelWriter(writers...)), rotator, nil
}

// consoleWriter returns a ConsoleWriter matching the app's stderr format.
// noColor strips ANSI codes — used for the file so it stays plain text.
func consoleWriter(out io.Writer, noColor bool) zerolog.ConsoleWriter {
	return zerolog.ConsoleWriter{Out: out, NoColor: noColor, TimeFormat: time.RFC3339}
}

// expandHome resolves a leading ~ to the user's home directory.
func expandHome(p string) string {
	if p != "~" && !strings.HasPrefix(p, "~/") {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return p
	}
	if p == "~" {
		return home
	}
	return filepath.Join(home, p[2:])
}

// swappableWriter is an io.Writer whose target can be swapped atomically.
type swappableWriter struct {
	w atomic.Pointer[io.Writer]
}

func (s *swappableWriter) store(w io.Writer) {
	s.w.Store(&w)
}

func (s *swappableWriter) Write(p []byte) (int, error) {
	if wp := s.w.Load(); wp != nil {
		return (*wp).Write(p)
	}
	return os.Stderr.Write(p)
}
