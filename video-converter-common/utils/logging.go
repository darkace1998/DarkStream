package utils

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/darkace1998/video-converter-common/constants"
)

// CorrelationIDKey is the context key for correlation IDs
type CorrelationIDKey struct{}

// ComponentLogLevels stores per-component log levels
var (
	componentLogLevels = make(map[string]slog.Level)
	componentLogMu     sync.RWMutex
	globalLogLevel     = slog.LevelInfo
)

// InitLogger initializes the global logger with the specified level and format.
// level should be one of the constants.LogLevel* constants.
// format should be one of the constants.LogFormat* constants.
func InitLogger(level, format string) {
	globalLogLevel = parseLogLevel(level)

	opts := &slog.HandlerOptions{
		Level: globalLogLevel,
	}

	var handler slog.Handler
	if format == constants.LogFormatJSON {
		handler = slog.NewJSONHandler(os.Stdout, opts)
	} else {
		handler = slog.NewTextHandler(os.Stdout, opts)
	}

	slog.SetDefault(slog.New(handler))
}

// SetComponentLogLevel sets the log level for a specific component.
// Components with a specific level will use that level instead of the global level.
func SetComponentLogLevel(component string, level string) {
	componentLogMu.Lock()
	defer componentLogMu.Unlock()
	componentLogLevels[component] = parseLogLevel(level)
}

// GetComponentLogLevel returns the log level for a specific component.
// If no specific level is set, returns the global log level.
func GetComponentLogLevel(component string) slog.Level {
	componentLogMu.RLock()
	defer componentLogMu.RUnlock()
	if level, ok := componentLogLevels[component]; ok {
		return level
	}
	return globalLogLevel
}

// GenerateCorrelationID generates a new correlation ID for request tracing
func GenerateCorrelationID() string {
	bytes := make([]byte, 8)
	if _, err := rand.Read(bytes); err != nil {
		// Fallback to timestamp + pid based ID if random fails
		now := time.Now().UnixNano()
		pid := os.Getpid()
		fallbackBytes := []byte{
			byte(now >> 56), byte(now >> 48), byte(now >> 40), byte(now >> 32),
			byte(now >> 24), byte(now >> 16), byte(now >> 8), byte(now),
			byte(pid >> 8), byte(pid),
		}
		return "fb-" + hex.EncodeToString(fallbackBytes)
	}
	return hex.EncodeToString(bytes)
}

// ContextWithCorrelationID adds a correlation ID to the context
func ContextWithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, CorrelationIDKey{}, correlationID)
}

// CorrelationIDFromContext retrieves the correlation ID from context
func CorrelationIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(CorrelationIDKey{}).(string); ok {
		return id
	}
	return ""
}

// ComponentLogger creates a logger with component name and optional correlation ID
type ComponentLogger struct {
	component     string
	correlationID string
	attrs         []any
}

// NewComponentLogger creates a new component logger
func NewComponentLogger(component string) *ComponentLogger {
	return &ComponentLogger{
		component: component,
		attrs:     make([]any, 0, 8), // Pre-allocate for common use cases
	}
}

// WithCorrelationID adds a correlation ID to the logger
func (l *ComponentLogger) WithCorrelationID(correlationID string) *ComponentLogger {
	return &ComponentLogger{
		component:     l.component,
		correlationID: correlationID,
		attrs:         l.attrs,
	}
}

// WithContext extracts correlation ID from context
func (l *ComponentLogger) WithContext(ctx context.Context) *ComponentLogger {
	correlationID := CorrelationIDFromContext(ctx)
	return l.WithCorrelationID(correlationID)
}

// With adds additional attributes to the logger
func (l *ComponentLogger) With(args ...any) *ComponentLogger {
	newAttrs := make([]any, len(l.attrs)+len(args))
	copy(newAttrs, l.attrs)
	copy(newAttrs[len(l.attrs):], args)
	return &ComponentLogger{
		component:     l.component,
		correlationID: l.correlationID,
		attrs:         newAttrs,
	}
}

// buildArgs prepends component and correlation ID to log args
func (l *ComponentLogger) buildArgs(args []any) []any {
	baseArgs := make([]any, 0, len(args)+len(l.attrs)+4)
	baseArgs = append(baseArgs, "component", l.component)
	if l.correlationID != "" {
		baseArgs = append(baseArgs, "correlation_id", l.correlationID)
	}
	baseArgs = append(baseArgs, l.attrs...)
	baseArgs = append(baseArgs, args...)
	return baseArgs
}

// shouldLog checks if logging is enabled for this component at the given level
func (l *ComponentLogger) shouldLog(level slog.Level) bool {
	return level >= GetComponentLogLevel(l.component)
}

// Debug logs a debug message
func (l *ComponentLogger) Debug(msg string, args ...any) {
	if l.shouldLog(slog.LevelDebug) {
		slog.Debug(msg, l.buildArgs(args)...)
	}
}

// Info logs an info message
func (l *ComponentLogger) Info(msg string, args ...any) {
	if l.shouldLog(slog.LevelInfo) {
		slog.Info(msg, l.buildArgs(args)...)
	}
}

// Warn logs a warning message
func (l *ComponentLogger) Warn(msg string, args ...any) {
	if l.shouldLog(slog.LevelWarn) {
		slog.Warn(msg, l.buildArgs(args)...)
	}
}

// Error logs an error message
func (l *ComponentLogger) Error(msg string, args ...any) {
	if l.shouldLog(slog.LevelError) {
		slog.Error(msg, l.buildArgs(args)...)
	}
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
