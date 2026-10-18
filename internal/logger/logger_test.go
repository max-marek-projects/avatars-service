package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/trace"
)

// ---------- Level ----------

func TestLevelSet(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLevel Level
		wantErr   bool
	}{
		{"valid DEBUG", "DEBUG", LevelDebug, false},
		{"valid INFO", "INFO", LevelInfo, false},
		{"valid WARN", "WARN", LevelWarn, false},
		{"valid ERROR", "ERROR", LevelError, false},
		{"invalid lower", "debug", "", true},
		{"invalid value", "FATAL", "", true},
		{"empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l Level
			err := l.Set(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid log level")
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantLevel, l)
			}
		})
	}
}

func TestLevelString(t *testing.T) {
	assert.Equal(t, "DEBUG", LevelDebug.String())
	assert.Equal(t, "INFO", LevelInfo.String())
	assert.Equal(t, "WARN", LevelWarn.String())
	assert.Equal(t, "ERROR", LevelError.String())
}

func TestLevelMarshalUnmarshal(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		data, err := LevelDebug.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, "DEBUG", string(data))
	})

	t.Run("unmarshal", func(t *testing.T) {
		var l Level
		err := l.UnmarshalText([]byte("WARN"))
		require.NoError(t, err)
		assert.Equal(t, LevelWarn, l)
	})

	t.Run("unmarshal invalid", func(t *testing.T) {
		var l Level
		err := l.UnmarshalText([]byte("invalid"))
		assert.Error(t, err)
	})
}

// ---------- New / slog.Default ----------

func TestInitialize(t *testing.T) {
	tests := []struct {
		name    string
		level   Level
		wantLvl slog.Level
		wantErr bool
	}{
		{"valid DEBUG", LevelDebug, slog.LevelDebug, false},
		{"valid INFO", LevelInfo, slog.LevelInfo, false},
		{"valid WARN", LevelWarn, slog.LevelWarn, false},
		{"valid ERROR", LevelError, slog.LevelError, false},
		{"invalid level", Level("FATAL"), 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.level)
			if tt.wantErr {
				require.Error(t, err)
				assert.Contains(t, err.Error(), "invalid log level")
				return
			}
			require.NoError(t, err)
			require.NotNil(t, logger)

			assert.Same(t, logger, slog.Default())

			h := logger.Handler()
			th, ok := h.(*traceHandler)
			require.True(t, ok, "top handler must be *traceHandler, got %T", h)

			_, ok = th.next.(*slog.JSONHandler)
			require.True(t, ok, "traceHandler.next must be *slog.JSONHandler, got %T", th.next)

			ctx := context.Background()
			assert.True(t, logger.Enabled(ctx, tt.wantLvl))
		})
	}
}

// ---------- traceHandler: trace_id / span_id ----------
func newBufferedLogger(t *testing.T, level slog.Level, buf *bytes.Buffer) *slog.Logger {
	t.Helper()
	base := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: level})
	return slog.New(&traceHandler{next: base})
}

func TestTraceHandler_InjectsTraceAndSpanIDs(t *testing.T) {
	var buf bytes.Buffer
	log := newBufferedLogger(t, slog.LevelDebug, &buf)

	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	require.NoError(t, err)
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	require.NoError(t, err)

	sc := trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	})
	ctx := trace.ContextWithSpanContext(context.Background(), sc)

	log.InfoContext(ctx, "hello")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	assert.Equal(t, "hello", entry["msg"])
	assert.Equal(t, "INFO", entry["level"])
	assert.Equal(t, traceID.String(), entry["trace_id"])
	assert.Equal(t, spanID.String(), entry["span_id"])
}

func TestTraceHandler_NoSpan_NoTraceAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := newBufferedLogger(t, slog.LevelDebug, &buf)

	log.InfoContext(context.Background(), "no span here")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	assert.NotContains(t, entry, "trace_id")
	assert.NotContains(t, entry, "span_id")
}

func TestTraceHandler_InvalidSpanContext_NoTraceAttrs(t *testing.T) {
	var buf bytes.Buffer
	log := newBufferedLogger(t, slog.LevelDebug, &buf)

	ctx := trace.ContextWithSpanContext(context.Background(), trace.SpanContext{})

	log.InfoContext(ctx, "empty sc")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	assert.NotContains(t, entry, "trace_id")
	assert.NotContains(t, entry, "span_id")
}

func TestTraceHandler_WithAttrs_PreservesWrapper(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	root := &traceHandler{next: base}

	derived := root.WithAttrs([]slog.Attr{slog.String("component", "test")})
	_, ok := derived.(*traceHandler)
	require.True(t, ok, "WithAttrs must return *traceHandler, got %T", derived)

	log := slog.New(derived)

	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	log.InfoContext(ctx, "msg")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	assert.Equal(t, "test", entry["component"], "WithAttrs attribute must survive")
	assert.Equal(t, traceID.String(), entry["trace_id"], "trace_id must still be injected")
	assert.Equal(t, spanID.String(), entry["span_id"])
}

func TestTraceHandler_WithGroup_PreservesWrapper(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, nil)
	root := &traceHandler{next: base}

	grouped := root.WithGroup("g")
	_, ok := grouped.(*traceHandler)
	require.True(t, ok, "WithGroup must return *traceHandler, got %T", grouped)

	log := slog.New(grouped)

	traceID, _ := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	spanID, _ := trace.SpanIDFromHex("0102030405060708")
	ctx := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	log.InfoContext(ctx, "msg")

	var entry map[string]any
	require.NoError(t, json.Unmarshal(buf.Bytes(), &entry))

	g, ok := entry["g"].(map[string]any)
	require.True(t, ok, "group must be present in output")
	assert.Equal(t, traceID.String(), g["trace_id"])
	assert.Equal(t, spanID.String(), g["span_id"])
}

func TestTraceHandler_Enabled_Delegates(t *testing.T) {
	var buf bytes.Buffer
	base := slog.NewJSONHandler(&buf, &slog.HandlerOptions{Level: slog.LevelWarn})
	th := &traceHandler{next: base}

	ctx := context.Background()
	assert.False(t, th.Enabled(ctx, slog.LevelInfo), "Info must be disabled at WARN level")
	assert.True(t, th.Enabled(ctx, slog.LevelWarn), "Warn must be enabled at WARN level")
	assert.True(t, th.Enabled(ctx, slog.LevelError), "Error must be enabled at WARN level")
}

// ---------- flag.Value ----------

func TestFlagIntegration(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var l Level
	fs.Var(&l, "level", "log level")

	err := fs.Parse([]string{"-level", "DEBUG"})
	require.NoError(t, err)
	assert.Equal(t, LevelDebug, l)

	err = fs.Parse([]string{"-level", "FATAL"})
	assert.Error(t, err)
}
