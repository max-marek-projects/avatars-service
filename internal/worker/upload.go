package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/max-marek-projects/avatars-service/internal/metrics"
	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/max-marek-projects/avatars-service/internal/repository"
	"github.com/max-marek-projects/avatars-service/internal/tracing"
)

// handleUploadWithRetry runs handleUpload up to MaxRetries times with
// exponential backoff. After the final failure it marks the avatar as failed
// and returns the error to the consumer (which Nacks without requeue).
func (w *Worker) handleUploadWithRetry(ctx context.Context, ev models.AvatarUploadEvent) error {
	delay := w.cfg.RetryBaseDelay
	var lastErr error

	for attempt := 1; attempt <= w.cfg.MaxRetries; attempt++ {
		err := w.handleUpload(ctx, ev)
		if err == nil {
			return nil
		}
		lastErr = err

		if errors.Is(err, repository.ErrAvatarNotFound) {
			w.logger.WarnContext(ctx, "worker: avatar gone, dropping event",
				slog.String("avatar_id", ev.AvatarID))
			return nil
		}

		w.logger.WarnContext(ctx, "worker: upload processing attempt failed",
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

	avatarID, parseErr := uuid.Parse(ev.AvatarID)
	if parseErr == nil {
		if updErr := w.storage.UpdateProcessingStatus(ctx, avatarID, models.ProcessingStatusFailed, nil); updErr != nil {
			w.logger.ErrorContext(ctx, "worker: failed to mark processing_status=failed",
				slog.String("avatar_id", ev.AvatarID),
				slog.Any("error", updErr))
		}
	}
	return fmt.Errorf("%w: %v", ErrProcessingFailed, lastErr)
}

// handleUpload performs the actual work for one upload event.
//
//  1. Load metadata; skip if already completed (idempotency).
//  2. Download the original from S3.
//  3. Generate thumbnails for each configured size.
//  4. Upload thumbnails to S3.
//  5. Persist thumbnail keys and set processing_status=completed.
func (w *Worker) handleUpload(ctx context.Context, ev models.AvatarUploadEvent) error {
	ctx, span := tracer.Start(ctx, "Worker.handleUpload",
		trace.WithAttributes(
			attribute.String("avatar.id", ev.AvatarID),
			attribute.String("user.id", ev.UserID),
			attribute.String("s3.key", ev.S3Key),
		),
	)
	defer span.End()

	// Observe per-attempt latency. handleUploadWithRetry calls this in a loop,
	// so retried events produce one sample per attempt — useful for spotting
	// "slow first try, fast retry" patterns caused by transient S3/DB hiccups.
	start := time.Now()
	defer func() {
		metrics.AvatarProcessingDuration.WithLabelValues("upload").
			Observe(time.Since(start).Seconds())
	}()

	if ev.AvatarID == "" || ev.S3Key == "" {
		err := fmt.Errorf("worker: empty upload event")
		tracing.RecordError(span, err)
		return err
	}
	id, err := uuid.Parse(ev.AvatarID)
	if err != nil {
		err = fmt.Errorf("worker: invalid avatar id %q: %w", ev.AvatarID, err)
		tracing.RecordError(span, err)
		return err
	}

	avatar, err := w.storage.GetAvatarByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			// Domain error — normal business outcome, do not pollute error-rate.
			return repository.ErrAvatarNotFound
		}
		tracing.RecordError(span, err)
		return fmt.Errorf("worker: get avatar: %w", err)
	}
	if avatar.ProcessingStatus == models.ProcessingStatusCompleted {
		w.logger.InfoContext(ctx, "worker: upload already completed, skipping",
			slog.String("avatar_id", ev.AvatarID))
		return nil
	}

	if err := w.storage.UpdateProcessingStatus(ctx, id, models.ProcessingStatusProcessing, nil); err != nil {
		w.logger.WarnContext(ctx, "worker: could not mark processing",
			slog.String("avatar_id", ev.AvatarID), slog.Any("error", err))
	}

	src, err := w.objects.Download(ctx, ev.S3Key)
	if err != nil {
		err = fmt.Errorf("download original: %w", err)
		tracing.RecordError(span, err)
		return err
	}
	defer src.Close()

	raw, err := io.ReadAll(src)
	if err != nil {
		err = fmt.Errorf("read original: %w", err)
		tracing.RecordError(span, err)
		return err
	}

	thumbs := make(map[string]string, len(w.cfg.ThumbnailSizes))
	for _, size := range w.cfg.ThumbnailSizes {
		data, err := Resize(bytes.NewReader(raw), size)
		if err != nil {
			err = fmt.Errorf("resize %dx%d: %w", size, size, err)
			tracing.RecordError(span, err)
			return err
		}
		key := fmt.Sprintf("thumbnails/%s/%dx%d.jpg", ev.AvatarID, size, size)

		if err := w.objects.Upload(ctx, key, bytes.NewReader(data), int64(len(data)), "image/jpeg"); err != nil {
			err = fmt.Errorf("upload thumbnail %s: %w", key, err)
			tracing.RecordError(span, err)
			return err
		}
		thumbs[fmt.Sprintf("%dx%d", size, size)] = key
	}

	if err := w.storage.UpdateProcessingStatus(ctx, id, models.ProcessingStatusCompleted, thumbs); err != nil {
		err = fmt.Errorf("update processing status: %w", err)
		tracing.RecordError(span, err)
		return err
	}

	span.SetAttributes(attribute.Int("thumbnails.count", len(thumbs)))
	w.logger.InfoContext(ctx, "worker: upload processed",
		slog.String("avatar_id", ev.AvatarID),
		slog.Int("thumbnails", len(thumbs)))
	return nil
}
