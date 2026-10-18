// Package tracing initializes OpenTelemetry tracing with an OTLP HTTP exporter.
//
// If Config.Endpoint is empty, Init returns a no-op shutdown so the caller
// can wire it unconditionally (handy for local runs without a collector).
package tracing

import (
	"context"
	"fmt"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
)

// Config holds tracing initialization parameters.
type Config struct {
	// Endpoint is the OTLP HTTP collector host:port (e.g. "jaeger:4318").
	// Empty disables tracing entirely (Init returns a no-op shutdown).
	Endpoint string

	// ServiceName is reported as the service.name resource attribute.
	ServiceName string

	// ServiceVersion is reported as the service.version resource attribute.
	ServiceVersion string

	// SampleRatio is the fraction of traces to sample, 0 < r <= 1.
	// Values outside the range fall back to 1.0.
	SampleRatio float64

	// Insecure disables TLS on the OTLP connection (required for local Jaeger).
	Insecure bool
}

// Shutdown flushes pending spans and stops the tracer provider.
type Shutdown func(context.Context) error

// Init builds a TracerProvider, installs it as the global provider and
// sets the W3C TraceContext + Baggage propagators. Safe to call with an
// empty Endpoint — returns a no-op shutdown.
func Init(ctx context.Context, cfg Config) (Shutdown, error) {
	noop := Shutdown(func(context.Context) error { return nil })

	if cfg.Endpoint == "" {
		return noop, nil
	}
	if cfg.ServiceName == "" {
		cfg.ServiceName = "avatars-service"
	}
	if cfg.SampleRatio <= 0 || cfg.SampleRatio > 1 {
		cfg.SampleRatio = 1.0
	}

	opts := []otlptracehttp.Option{
		otlptracehttp.WithEndpoint(cfg.Endpoint),
	}
	if cfg.Insecure {
		opts = append(opts, otlptracehttp.WithInsecure())
	}

	exp, err := otlptracehttp.New(ctx, opts...)
	if err != nil {
		return noop, fmt.Errorf("otlp exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithFromEnv(),
		resource.WithTelemetrySDK(),
		resource.WithProcess(),
		resource.WithOS(),
		resource.WithHost(),
		resource.WithAttributes(
			semconv.ServiceName(cfg.ServiceName),
			semconv.ServiceVersion(cfg.ServiceVersion),
		),
	)
	if err != nil {
		return noop, fmt.Errorf("resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exp, sdktrace.WithBatchTimeout(3*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.ParentBased(
			sdktrace.TraceIDRatioBased(cfg.SampleRatio),
		)),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	return tp.Shutdown, nil
}
