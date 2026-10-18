// Package tracing — shared span helpers used by services and worker.
package tracing

import (
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// RecordError records an infrastructure error on the span and marks it failed.
// Do NOT call this for domain errors (not found, forbidden, validation) —
// those are normal business outcomes and would pollute the error-rate metrics.
func RecordError(span trace.Span, err error) {
	if err == nil || span == nil {
		return
	}
	span.RecordError(err)
	span.SetStatus(codes.Error, err.Error())
}
