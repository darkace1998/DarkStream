// Package logger provides logging initialization for workers.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Init initializes the logger with the specified level and format
func Init(level, format, outputPath string, attrs ...any) {
	opts := &slog.HandlerOptions{
		Level: parseLogLevel(level),
	}

	writer := io.Writer(os.Stdout)
	if outputPath != "" {
		if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "darkstream: failed to create log directory %q: %v\n", filepath.Dir(outputPath), err)
		} else if file, err := os.OpenFile(outputPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
			fmt.Fprintf(os.Stderr, "darkstream: failed to open log file %q: %v\n", outputPath, err)
		} else {
			writer = io.MultiWriter(os.Stdout, file)
		}
	}

	var handler slog.Handler
	var unsupportedFormat bool
	if format == "json" {
		handler = slog.NewJSONHandler(writer, opts)
	} else {
		if format != "text" && format != "" {
			unsupportedFormat = true
		}
		handler = slog.NewTextHandler(writer, opts)
	}

	base := slog.New(handler)
	if len(attrs) > 0 {
		base = base.With(attrs...)
	}

	slog.SetDefault(base)

	if unsupportedFormat {
		fmt.Fprintf(os.Stderr, "darkstream: unsupported log format %q, defaulting to text\n", format)
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
