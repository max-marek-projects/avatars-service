// Package logger provides a global slog.Logger instance with configurable log level.
// It defines a custom Level type that implements flag.Value and TextMarshaler/Unmarshaler
// for seamless integration with command-line flags, environment variables, and JSON.
package logger

import (
	"fmt"
	"log/slog"
	"os"
)

// Level represents a log level string (DEBUG, INFO, WARN, ERROR).
type Level string

// Level constants.
const (
	LevelDebug Level = "DEBUG"
	LevelInfo  Level = "INFO"
	LevelWarn  Level = "WARN"
	LevelError Level = "ERROR"
)

// logLevels maps Level constants to slog.Level values.
var logLevels = map[Level]slog.Level{
	LevelDebug: slog.LevelDebug,
	LevelInfo:  slog.LevelInfo,
	LevelWarn:  slog.LevelWarn,
	LevelError: slog.LevelError,
}

// String returns the string representation of the level.
func (l Level) String() string {
	return string(l)
}

// Set implements flag.Value interface.
// It validates that the given string is one of the predefined levels.
func (l *Level) Set(value string) error {
	switch Level(value) {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
		*l = Level(value)
		return nil
	default:
		return fmt.Errorf("invalid log level: %s", value)
	}
}

// UnmarshalText implements encoding.TextUnmarshaler for JSON and environment variables.
func (l *Level) UnmarshalText(text []byte) error {
	return l.Set(string(text))
}

// MarshalText implements encoding.TextMarshaler for JSON serialization.
func (l Level) MarshalText() ([]byte, error) {
	return []byte(l), nil
}

// New creates a new slog.Logger with a JSON handler writing to os.Stdout
// and sets it as the slog default logger.
//
// Parameters:
//   - level: the log level (one of LevelDebug, LevelInfo, LevelWarn, LevelError).
//
// Returns:
//   - *slog.Logger: the root logger instance.
//   - error: non-nil if the level is invalid.
func New(level Level) (*slog.Logger, error) {
	lvl, exists := logLevels[level]
	if !exists {
		return nil, fmt.Errorf("invalid log level: %s", level)
	}
	opts := &slog.HandlerOptions{Level: lvl}
	handler := slog.NewJSONHandler(os.Stdout, opts)
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, nil
}
