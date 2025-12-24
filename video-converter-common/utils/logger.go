package utils

import (
	"context"
	"io"
	"log/slog"
	"os"

	"github.com/darkace1998/video-converter-common/constants"
)

// Logger wraps slog.Logger with context support and additional features.
type Logger struct {
	logger *slog.Logger
}

// LoggerOption is a functional option for configuring Logger.
type LoggerOption func(*Logger)

// NewLogger creates a new Logger with the specified options.
func NewLogger(level, format string, opts ...LoggerOption) *Logger {
	l := &Logger{}

	handlerOpts := &slog.HandlerOptions{
		Level: parseLogLevelForLogger(level),
	}

	var handler slog.Handler
	if format == constants.LogFormatJSON {
		handler = slog.NewJSONHandler(os.Stdout, handlerOpts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, handlerOpts)
	}

	l.logger = slog.New(handler)

	for _, opt := range opts {
		opt(l)
	}

	return l
}

// NewLoggerWithWriter creates a new Logger that writes to the specified writer.
func NewLoggerWithWriter(w io.Writer, level, format string) *Logger {
	handlerOpts := &slog.HandlerOptions{
		Level: parseLogLevelForLogger(level),
	}

	var handler slog.Handler
	if format == constants.LogFormatJSON {
		handler = slog.NewJSONHandler(w, handlerOpts)
	} else {
		handler = slog.NewTextHandler(w, handlerOpts)
	}

	return &Logger{logger: slog.New(handler)}
}

func parseLogLevelForLogger(level string) slog.Level {
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

// WithContext returns a logger with context values attached.
func (l *Logger) WithContext(ctx context.Context) *Logger {
	attrs := extractContextAttrs(ctx)
	if len(attrs) == 0 {
		return l
	}
	return &Logger{logger: l.logger.With(attrs...)}
}

// With returns a logger with the specified attributes attached.
func (l *Logger) With(args ...any) *Logger {
	return &Logger{logger: l.logger.With(args...)}
}

// WithGroup returns a logger with a new group.
func (l *Logger) WithGroup(name string) *Logger {
	return &Logger{logger: l.logger.WithGroup(name)}
}

// Debug logs a debug message.
func (l *Logger) Debug(msg string, args ...any) {
	l.logger.Debug(msg, args...)
}

// DebugContext logs a debug message with context.
func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.logger.DebugContext(ctx, msg, args...)
}

// Info logs an info message.
func (l *Logger) Info(msg string, args ...any) {
	l.logger.Info(msg, args...)
}

// InfoContext logs an info message with context.
func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.logger.InfoContext(ctx, msg, args...)
}

// Warn logs a warning message.
func (l *Logger) Warn(msg string, args ...any) {
	l.logger.Warn(msg, args...)
}

// WarnContext logs a warning message with context.
func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.logger.WarnContext(ctx, msg, args...)
}

// Error logs an error message.
func (l *Logger) Error(msg string, args ...any) {
	l.logger.Error(msg, args...)
}

// ErrorContext logs an error message with context.
func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.logger.ErrorContext(ctx, msg, args...)
}

// Context keys for logger values
type contextKey string

const (
	// LoggerRequestIDKey is the context key for request ID.
	LoggerRequestIDKey contextKey = "request_id"
	// LoggerJobIDKey is the context key for job ID.
	LoggerJobIDKey contextKey = "job_id"
	// LoggerWorkerIDKey is the context key for worker ID.
	LoggerWorkerIDKey contextKey = "worker_id"
)

// ContextWithRequestID returns a context with a request ID.
func ContextWithRequestID(ctx context.Context, requestID string) context.Context {
	return context.WithValue(ctx, LoggerRequestIDKey, requestID)
}

// ContextWithJobID returns a context with a job ID.
func ContextWithJobID(ctx context.Context, jobID string) context.Context {
	return context.WithValue(ctx, LoggerJobIDKey, jobID)
}

// ContextWithWorkerID returns a context with a worker ID.
func ContextWithWorkerID(ctx context.Context, workerID string) context.Context {
	return context.WithValue(ctx, LoggerWorkerIDKey, workerID)
}

// extractContextAttrs extracts logger attributes from context.
func extractContextAttrs(ctx context.Context) []any {
	var attrs []any

	if v := ctx.Value(LoggerRequestIDKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			attrs = append(attrs, slog.String("request_id", s))
		}
	}
	if v := ctx.Value(LoggerJobIDKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			attrs = append(attrs, slog.String("job_id", s))
		}
	}
	if v := ctx.Value(LoggerWorkerIDKey); v != nil {
		if s, ok := v.(string); ok && s != "" {
			attrs = append(attrs, slog.String("worker_id", s))
		}
	}

	return attrs
}

// SetDefault sets the logger as the default slog logger.
func (l *Logger) SetDefault() {
	slog.SetDefault(l.logger)
}

// Slog returns the underlying slog.Logger.
func (l *Logger) Slog() *slog.Logger {
	return l.logger
}
