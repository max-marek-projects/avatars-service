package tracing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/codes"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/max-marek-projects/avatars-service/internal/tracing"
)

func newTestProvider() (*sdktrace.TracerProvider, *tracetest.SpanRecorder) {
	sr := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(sr))
	return tp, sr
}

func TestRecordError_NilErrorIsNoOp(t *testing.T) {
	tp, sr := newTestProvider()
	_, span := tp.Tracer("test").Start(context.Background(), "op")

	tracing.RecordError(span, nil)
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)
	assert.Empty(t, spans[0].Events())
	assert.Equal(t, codes.Unset, spans[0].Status().Code)
}

func TestRecordError_NilSpanIsNoOp(t *testing.T) {
	tracing.RecordError(nil, errors.New("boom"))
}

func TestRecordError_RecordsEventAndSetsErrorStatus(t *testing.T) {
	tp, sr := newTestProvider()
	_, span := tp.Tracer("test").Start(context.Background(), "op")

	err := errors.New("infra failure")
	tracing.RecordError(span, err)
	span.End()

	spans := sr.Ended()
	require.Len(t, spans, 1)

	require.Len(t, spans[0].Events(), 1)
	assert.Equal(t, "exception", spans[0].Events()[0].Name)

	assert.Equal(t, codes.Error, spans[0].Status().Code)
	assert.Contains(t, spans[0].Status().Description, "infra failure")
}
