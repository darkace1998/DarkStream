// Package logger provides logging initialization for the master coordinator.
package logger

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
)

// Init initializes the logger with the specified level and format
func Init(level, format, outputPath string) {
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

	if format != "json" && format != "text" && format != "" {
		fmt.Fprintf(os.Stderr, "darkstream: unsupported log format %q, defaulting to text\n", format)
	}

	var handler slog.Handler
	if format == "json" {
		handler = slog.NewJSONHandler(writer, opts)
	} else {
		handler = slog.NewTextHandler(writer, opts)
	}

	slog.SetDefault(slog.New(handler))
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
