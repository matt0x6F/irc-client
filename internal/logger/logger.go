package logger

import (
	"os"
	"time"

	"github.com/rs/zerolog"
)

var Log zerolog.Logger

func init() {
	// Configure ZeroLog in text mode with colors
	Log = zerolog.New(zerolog.ConsoleWriter{
		Out:        os.Stderr,
		NoColor:    false,
		TimeFormat: time.RFC3339,
	}).With().Timestamp().Logger()

	// Set default log level to Info
	zerolog.SetGlobalLevel(zerolog.InfoLevel)
}

// SetLevel sets the global log level
func SetLevel(level zerolog.Level) {
	zerolog.SetGlobalLevel(level)
}

