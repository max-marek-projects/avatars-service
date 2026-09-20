package worker

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/max-marek-projects/avatars-service/internal/models"
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
		w.logger.Warn("worker: delete attempt failed",
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
	if ev.AvatarID == "" {
		return fmt.Errorf("worker: empty delete event")
	}
	if len(ev.S3Keys) == 0 {
		w.logger.Info("worker: nothing to delete", slog.String("avatar_id", ev.AvatarID))
		return nil
	}

	var firstErr error
	for _, key := range ev.S3Keys {
		key = strings.TrimSpace(key)
		if key == "" {
			continue
		}
		if err := w.objects.Delete(ctx, key); err != nil {
			// Some S3 SDKs return a "NoSuchKey" / 404 error; treat as success.
			if isNotFound(err) {
				w.logger.Info("worker: object already gone",
					slog.String("key", key))
				continue
			}
			w.logger.Error("worker: delete object failed",
				slog.String("key", key), slog.Any("error", err))
			if firstErr == nil {
				firstErr = fmt.Errorf("delete %s: %w", key, err)
			}
		}
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
