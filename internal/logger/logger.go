// Package logger provides a global slog.Logger instance with configurable
// log level and automatic correlation with OpenTelemetry traces.
//
// Every log record written via logger.InfoContext(ctx, ...) / ...Context
// is automatically enriched with trace_id and span_id taken from the
// active span in ctx.
package logger

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
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

var logLevels = map[Level]slog.Level{
	LevelDebug: slog.LevelDebug,
	LevelInfo:  slog.LevelInfo,
	LevelWarn:  slog.LevelWarn,
	LevelError: slog.LevelError,
}

func (l Level) String() string { return string(l) }

func (l *Level) Set(value string) error {
	switch Level(value) {
	case LevelDebug, LevelInfo, LevelWarn, LevelError:
		*l = Level(value)
		return nil
	default:
		return fmt.Errorf("invalid log level: %s", value)
	}
}

func (l *Level) UnmarshalText(text []byte) error { return l.Set(string(text)) }

func (l Level) MarshalText() ([]byte, error) { return []byte(l), nil }

// New builds a JSON slog.Logger that automatically attaches trace_id
// and span_id to every record whose context carries a valid span.
// The returned logger is also installed as slog.Default().
func New(level Level) (*slog.Logger, error) {
	lvl, exists := logLevels[level]
	if !exists {
		return nil, fmt.Errorf("invalid log level: %s", level)
	}

	base := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: lvl})
	handler := &traceHandler{next: base}

	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger, nil
}

// ---------- trace correlation ----------

// traceHandler wraps a slog.Handler and adds trace_id / span_id attributes
// to each record based on the SpanContext stored in the record's context.
type traceHandler struct {
	next slog.Handler
}

// Enabled delegates to the underlying handler.
func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.next.Enabled(ctx, level)
}

// Handle injects trace attributes (if any) and forwards the record.
func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.next.Handle(ctx, r)
}

// WithAttrs returns a new handler with the given attributes pre-attached.
// The returned handler must remain a traceHandler, otherwise the chain
// breaks after a call like logger.With("component", "worker").
func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{next: h.next.WithAttrs(attrs)}
}

// WithGroup returns a new handler scoped to a group.
func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{next: h.next.WithGroup(name)}
}
