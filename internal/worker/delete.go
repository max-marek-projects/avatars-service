package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/max-marek-projects/avatars-service/internal/metrics"
	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/max-marek-projects/avatars-service/internal/tracing"
)

// handleDeleteWithRetry wraps handleDelete with exponential backoff.
func (w *Worker) handleDeleteWithRetry(ctx context.Context, ev models.AvatarDeleteEvent) error {
	delay := w.cfg.RetryBaseDelay
	var lastErr error

	for attempt := 1; attempt <= w.cfg.MaxRetries; attempt++ {
		err := w.handleDelete(ctx, ev)
		if err == nil {
			return nil
		}
		lastErr = err
		w.logger.WarnContext(ctx, "worker: delete attempt failed",
			slog.String("avatar_id", ev.AvatarID),
			slog.Int("attempt", attempt),
			slog.Duration("next_delay", delay),
			slog.Any("error", err))

		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(delay):
		}
		delay *= 2
	}
	return fmt.Errorf("%w: %v", ErrProcessingFailed, lastErr)
}

// handleDelete removes every S3 object listed in the event.
// Deleting a missing object is treated as success (S3 DELETE is idempotent).
func (w *Worker) handleDelete(ctx context.Context, ev models.AvatarDeleteEvent) error {
	ctx, span := tracer.Start(ctx, "Worker.handleDelete",
		trace.WithAttributes(
			attribute.String("avatar.id", ev.AvatarID),
			attribute.Int("s3.keys.count", len(ev.S3Keys)),
		),
	)
	defer span.End()

	start := time.Now()
	defer func() {
		metrics.AvatarProcessingDuration.WithLabelValues("delete").
			Observe(time.Since(start).Seconds())
	}()

	if ev.AvatarID == "" {
		err := fmt.Errorf("worker: empty delete event")
		tracing.RecordError(span, err)
		return err
	}
	if len(ev.S3Keys) == 0 {
		w.logger.InfoContext(ctx, "worker: nothing to delete", slog.String("avatar_id", ev.AvatarID))
		return nil
	}

	var firstErr error
	for _, key := range ev.S3Keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if err := w.objects.Delete(ctx, key); err != nil {
			if isNotFound(err) {
				w.logger.InfoContext(ctx, "worker: object already gone",
					slog.String("key", key))
				continue
			}
			w.logger.ErrorContext(ctx, "worker: delete object failed",
				slog.String("key", key), slog.Any("error", err))
			if firstErr == nil {
				firstErr = fmt.Errorf("delete %s: %w", key, err)
			}
		}
	}
	if firstErr != nil {
		tracing.RecordError(span, firstErr)
	}
	return firstErr
}

// isNotFound is a best-effort check for S3 "NoSuchKey" errors.
// The exact SDK type differs between minio-go and aws-sdk-go, so we match
// against the common code strings.
func isNotFound(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "NoSuchKey") ||
		strings.Contains(msg, "StatusCode: 404") ||
		strings.Contains(msg, "not found")
}
