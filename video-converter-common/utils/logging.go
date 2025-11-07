package utils

import (
	"log/slog"
	"os"

	"github.com/darkace1998/video-converter-common/constants"
)

// InitLogger initializes the global logger with the specified level and format.
// level should be one of the constants.LogLevel* constants.
// format should be one of the constants.LogFormat* constants.
func InitLogger(level, format string) {
	opts := &slog.HandlerOptions{
		Level: parseLogLevel(level),
	}

	var handler slog.Handler
	if format == constants.LogFormatJSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case constants.LogLevelDebug:
		return slog.LevelDebug
	case constants.LogLevelInfo:
		return slog.LevelInfo
	case constants.LogLevelWarn:
		return slog.LevelWarn
	case constants.LogLevelError:
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
