package logger

import (
	"log/slog"
	"os"
)

// Init initializes the logger with the specified level and format
func Init(level, format string) {
	opts := &slog.HandlerOptions{
		Level: parseLogLevel(level),
	}

	var handler slog.Handler
	var unsupportedFormat bool
	if format == "json" {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		if format != "text" && format != "" {
			unsupportedFormat = true
		}
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))

	if unsupportedFormat {
		slog.Warn("Unsupported log format, defaulting to text", "format", format)
	}
}

func parseLogLevel(level string) slog.Level {
	switch level {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelInfo
	}
}
