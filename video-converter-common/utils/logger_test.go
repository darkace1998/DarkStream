package utils

import (
	"bytes"
	"context"
	"testing"

	"github.com/darkace1998/video-converter-common/constants"
)

func TestNewLogger(t *testing.T) {
	tests := []struct {
		name   string
		level  string
		format string
	}{
		{"debug json", constants.LogLevelDebug, constants.LogFormatJSON},
		{"info text", constants.LogLevelInfo, constants.LogFormatText},
		{"warn json", constants.LogLevelWarn, constants.LogFormatJSON},
		{"error text", constants.LogLevelError, constants.LogFormatText},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger := NewLogger(tt.level, tt.format)
			if logger == nil {
				t.Fatal("NewLogger() returned nil")
			}
			if logger.Slog() == nil {
				t.Error("Logger.Slog() returned nil")
			}
		})
	}
}

func TestNewLoggerWithWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter(&buf, constants.LogLevelInfo, constants.LogFormatText)

	logger.Info("test message")

	if buf.Len() == 0 {
		t.Error("Logger did not write to buffer")
	}
}

func TestLoggerWith(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter(&buf, constants.LogLevelDebug, constants.LogFormatText)

	childLogger := logger.With("key", "value")
	childLogger.Info("test message")

	output := buf.String()
	if len(output) == 0 {
		t.Error("Logger did not write to buffer")
	}
}

func TestLoggerWithGroup(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter(&buf, constants.LogLevelDebug, constants.LogFormatText)

	childLogger := logger.WithGroup("mygroup")
	childLogger.Info("test message", "key", "value")

	output := buf.String()
	if len(output) == 0 {
		t.Error("Logger did not write to buffer")
	}
}

func TestLoggerLevels(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter(&buf, constants.LogLevelDebug, constants.LogFormatText)

	logger.Debug("debug message")
	logger.Info("info message")
	logger.Warn("warn message")
	logger.Error("error message")

	output := buf.String()
	if len(output) == 0 {
		t.Error("Logger did not write any messages")
	}
}

func TestLoggerContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter(&buf, constants.LogLevelDebug, constants.LogFormatText)

	ctx := context.Background()
	ctx = ContextWithRequestID(ctx, "req-123")
	ctx = ContextWithJobID(ctx, "job-456")
	ctx = ContextWithWorkerID(ctx, "worker-789")

	ctxLogger := logger.WithContext(ctx)
	ctxLogger.Info("test message")

	output := buf.String()
	if len(output) == 0 {
		t.Error("Logger did not write to buffer")
	}
}

func TestLoggerContextMethods(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter(&buf, constants.LogLevelDebug, constants.LogFormatText)

	ctx := context.Background()
	ctx = ContextWithRequestID(ctx, "req-123")

	logger.DebugContext(ctx, "debug message")
	logger.InfoContext(ctx, "info message")
	logger.WarnContext(ctx, "warn message")
	logger.ErrorContext(ctx, "error message")

	output := buf.String()
	if len(output) == 0 {
		t.Error("Logger did not write any messages")
	}
}

func TestContextFunctions(t *testing.T) {
	ctx := context.Background()

	ctx = ContextWithRequestID(ctx, "req-123")
	if v := ctx.Value(LoggerRequestIDKey); v != "req-123" {
		t.Errorf("ContextWithRequestID: got %v, want req-123", v)
	}

	ctx = ContextWithJobID(ctx, "job-456")
	if v := ctx.Value(LoggerJobIDKey); v != "job-456" {
		t.Errorf("ContextWithJobID: got %v, want job-456", v)
	}

	ctx = ContextWithWorkerID(ctx, "worker-789")
	if v := ctx.Value(LoggerWorkerIDKey); v != "worker-789" {
		t.Errorf("ContextWithWorkerID: got %v, want worker-789", v)
	}
}

func TestLoggerWithEmptyContext(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLoggerWithWriter(&buf, constants.LogLevelDebug, constants.LogFormatText)

	ctx := context.Background()
	ctxLogger := logger.WithContext(ctx)

	// Should return the same logger when context has no values
	if ctxLogger == nil {
		t.Error("WithContext returned nil")
	}

	ctxLogger.Info("test message")
	output := buf.String()
	if len(output) == 0 {
		t.Error("Logger did not write to buffer")
	}
}

func TestParseLogLevel(t *testing.T) {
	// Test via NewLogger with different levels
	tests := []struct {
		level string
	}{
		{constants.LogLevelDebug},
		{constants.LogLevelInfo},
		{constants.LogLevelWarn},
		{constants.LogLevelError},
		{"unknown"}, // Should default to info
	}

	for _, tt := range tests {
		t.Run(tt.level, func(t *testing.T) {
			logger := NewLogger(tt.level, constants.LogFormatText)
			if logger == nil {
				t.Fatal("NewLogger() returned nil")
			}
		})
	}
}
