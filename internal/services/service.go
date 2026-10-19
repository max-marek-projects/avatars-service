// Package service implements the business logic of avatars-service:
// avatar upload, retrieval, listing and deletion, plus async job publishing.
package services

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/max-marek-projects/avatars-service/internal/metrics"
	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/max-marek-projects/avatars-service/internal/repository"
	"github.com/max-marek-projects/avatars-service/internal/tracing"
	"github.com/sony/gobreaker"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Config holds tunable service parameters.
type Config struct {
	// MaxFileSize is the maximum allowed upload size in bytes.
	MaxFileSize int64

	// AllowedMimeTypes is the whitelist of accepted content types.
	AllowedMimeTypes []string

	// DefaultAvatarKey is the S3 key of the placeholder image returned
	// for users without an uploaded avatar. Empty disables the placeholder.
	DefaultAvatarKey string
}

type service struct {
	storage   Storage
	objects   ObjectStorage
	publisher Publisher
	logger    *slog.Logger
	config    Config
	s3CB      *gobreaker.CircuitBreaker
	dbCB      *gobreaker.CircuitBreaker
}

// NewService builds a service instance and applies sane defaults.
func NewService(
	storage Storage,
	objects ObjectStorage,
	publisher Publisher,
	cfg Config,
	logger *slog.Logger,
) (*service, error) {
	if storage == nil {
		return nil, fmt.Errorf("nil storage: %w", ErrInvalidArgument)
	}
	if objects == nil {
		return nil, fmt.Errorf("nil object storage: %w", ErrInvalidArgument)
	}
	if publisher == nil {
		return nil, fmt.Errorf("nil publisher: %w", ErrInvalidArgument)
	}
	if logger == nil {
		logger = slog.Default()
	}
	if cfg.MaxFileSize <= 0 {
		cfg.MaxFileSize = 10 * 1024 * 1024 // 10 MB
	}
	if len(cfg.AllowedMimeTypes) == 0 {
		cfg.AllowedMimeTypes = []string{"image/jpeg", "image/png", "image/webp"}
	}

	cbSettings := gobreaker.Settings{
		Timeout: 10 * time.Second,
		ReadyToTrip: func(counts gobreaker.Counts) bool {
			return counts.ConsecutiveFailures >= 5
		},
	}

	return &service{
		storage:   storage,
		objects:   objects,
		publisher: publisher,
		logger:    logger,
		config:    cfg,
		s3CB:      gobreaker.NewCircuitBreaker(cbSettings),
		dbCB:      gobreaker.NewCircuitBreaker(cbSettings),
	}, nil
}

// ---------- Circuit Breaker helpers ----------

// withS3CB executes fn within the S3 circuit breaker.
func (s *service) withS3CB(fn func() error) error {
	_, err := s.s3CB.Execute(func() (interface{}, error) {
		return nil, fn()
	})
	return err
}

// withDBCB executes fn within the DB circuit breaker.
func (s *service) withDBCB(fn func() error) error {
	_, err := s.dbCB.Execute(func() (interface{}, error) {
		return nil, fn()
	})
	return err
}

// isCBOpen reports whether the error is caused by an open circuit breaker.
func isCBOpen(err error) bool {
	return errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests)
}

// ---------- Avatar upload ----------

// UploadAvatar streams the file to S3, stores metadata and publishes an
// AvatarUploadEvent for the worker. The returned avatar has processing_status
// set to "pending" until the worker creates thumbnails.
func (s *service) UploadAvatar(
	ctx context.Context,
	userID string,
	r io.Reader,
	fileName, contentType string,
	size int64,
) (_ *models.Avatar, err error) {
	// traces
	ctx, span := tracer.Start(ctx, "Service.UploadAvatar",
		trace.WithAttributes(
			attribute.String("user.id", userID),
			attribute.String("file.name", fileName),
			attribute.Int64("file.size_bytes", size),
			attribute.String("file.content_type", contentType),
		),
	)
	defer span.End()

	// metrics
	defer func() {
		metrics.AvatarUploadsTotal.WithLabelValues(classifyUploadError(err)).Inc()
		if err == nil {
			metrics.AvatarUploadBytes.Observe(float64(size))
		}
	}()

	if userID == "" {
		return nil, fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}
	if r == nil {
		return nil, fmt.Errorf("nil file body: %w", ErrInvalidArgument)
	}
	if fileName == "" {
		return nil, fmt.Errorf("empty file name: %w", ErrInvalidArgument)
	}
	if size <= 0 {
		return nil, fmt.Errorf("empty file: %w", ErrInvalidArgument)
	}
	if size > s.config.MaxFileSize {
		return nil, fmt.Errorf("%w: max %d bytes", ErrFileTooLarge, s.config.MaxFileSize)
	}
	if !s.isAllowedMime(contentType) {
		return nil, fmt.Errorf("%w: %s", ErrUnsupportedFormat, contentType)
	}

	id := uuid.New()
	key := s.buildOriginalKey(userID, id, fileName)
	span.SetAttributes(attribute.String("avatar.id", id.String()), attribute.String("s3.key", key))

	// 1. Upload original to S3 (with Circuit Breaker).
	if cbErr := s.withS3CB(func() error {
		return s.objects.Upload(ctx, key, r, size, contentType)
	}); cbErr != nil {
		tracing.RecordError(span, cbErr)
		if isCBOpen(cbErr) {
			return nil, ErrServiceUnavailable
		}
		return nil, fmt.Errorf("failed to upload file to S3: %w", cbErr)
	}

	// 2. Persist metadata (with Circuit Breaker).
	avatar := &models.Avatar{
		ID:               id,
		UserID:           userID,
		FileName:         fileName,
		MimeType:         contentType,
		SizeBytes:        size,
		S3Key:            key,
		ThumbnailS3Keys:  map[string]string{},
		UploadStatus:     models.UploadStatusUploaded,
		ProcessingStatus: models.ProcessingStatusPending,
	}
	if cbErr := s.withDBCB(func() error {
		return s.storage.CreateAvatar(ctx, avatar)
	}); cbErr != nil {
		// Rollback the S3 upload: we do not want orphan files.
		if delErr := s.objects.Delete(ctx, key); delErr != nil {
			s.logger.ErrorContext(ctx, "failed to rollback S3 upload",
				slog.String("key", key),
				slog.Any("error", delErr))
		}
		tracing.RecordError(span, cbErr)
		if isCBOpen(cbErr) {
			return nil, ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrAlreadyInStorage) {
			return nil, ErrAvatarConflict
		}
		if errors.Is(cbErr, repository.ErrInvalidArgument) {
			return nil, ErrInvalidArgument
		}
		return nil, fmt.Errorf("failed to store avatar metadata: %w", cbErr)
	}

	// 3. Publish event for the worker.
	event := models.AvatarUploadEvent{
		AvatarID: id.String(),
		UserID:   userID,
		S3Key:    key,
	}
	if err := s.publisher.PublishUpload(ctx, event); err != nil {
		s.logger.ErrorContext(ctx, "failed to publish upload event",
			slog.String("avatar_id", id.String()),
			slog.Any("error", err))
		if updErr := s.storage.UpdateUploadStatus(ctx, id, models.UploadStatusFailed); updErr != nil {
			s.logger.ErrorContext(ctx, "failed to mark avatar as failed",
				slog.String("avatar_id", id.String()),
				slog.Any("error", updErr))
		}
		tracing.RecordError(span, err)
		return nil, fmt.Errorf("failed to enqueue avatar for processing: %w", err)
	}
	return avatar, nil
}

// ---------- Read ----------

// GetAvatarByID returns the avatar and a streaming reader for the original file.
func (s *service) GetAvatarByID(ctx context.Context, id uuid.UUID) (*models.Avatar, io.ReadCloser, error) {
	// traces
	ctx, span := tracer.Start(ctx, "Service.GetAvatarByID",
		trace.WithAttributes(attribute.String("avatar.id", id.String())))
	defer span.End()

	if id == uuid.Nil {
		return nil, nil, fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}

	var avatar *models.Avatar
	if cbErr := s.withDBCB(func() error {
		var err error
		avatar, err = s.storage.GetAvatarByID(ctx, id)
		return err
	}); cbErr != nil {
		if isCBOpen(cbErr) {
			return nil, nil, ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrAvatarNotFound) {
			return nil, nil, ErrAvatarNotFound
		}
		if errors.Is(cbErr, repository.ErrInvalidArgument) {
			return nil, nil, ErrInvalidArgument
		}
		tracing.RecordError(span, cbErr)
		return nil, nil, fmt.Errorf("failed to get avatar: %w", cbErr)
	}

	var rc io.ReadCloser
	if cbErr := s.withS3CB(func() error {
		var err error
		rc, err = s.objects.Download(ctx, avatar.S3Key)
		return err
	}); cbErr != nil {
		tracing.RecordError(span, cbErr)
		if isCBOpen(cbErr) {
			return nil, nil, ErrServiceUnavailable
		}
		return nil, nil, fmt.Errorf("failed to download avatar: %w", cbErr)
	}
	return avatar, rc, nil
}

// GetActiveAvatar returns the latest avatar of the user. When the user
// has no uploaded avatar and DefaultAvatarKey is configured, the placeholder
// is returned instead (this is the main avatars-service use case).
func (s *service) GetActiveAvatar(ctx context.Context, userID string) (*models.Avatar, io.ReadCloser, error) {
	// traces
	ctx, span := tracer.Start(ctx, "Service.GetActiveAvatar",
		trace.WithAttributes(attribute.String("user.id", userID)))
	defer span.End()

	if userID == "" {
		return nil, nil, fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}

	var avatar *models.Avatar
	if cbErr := s.withDBCB(func() error {
		var err error
		avatar, err = s.storage.GetActiveAvatarByUserID(ctx, userID)
		return err
	}); cbErr != nil {
		if isCBOpen(cbErr) {
			return nil, nil, ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrAvatarNotFound) {
			return s.placeholder(ctx)
		}
		if errors.Is(cbErr, repository.ErrInvalidArgument) {
			return nil, nil, ErrInvalidArgument
		}
		tracing.RecordError(span, cbErr)
		return nil, nil, fmt.Errorf("failed to get active avatar: %w", cbErr)
	}

	var rc io.ReadCloser
	if cbErr := s.withS3CB(func() error {
		var err error
		rc, err = s.objects.Download(ctx, avatar.S3Key)
		return err
	}); cbErr != nil {
		tracing.RecordError(span, cbErr)
		if isCBOpen(cbErr) {
			return nil, nil, ErrServiceUnavailable
		}
		return nil, nil, fmt.Errorf("failed to download avatar: %w", cbErr)
	}
	return avatar, rc, nil
}

// GetAvatarMetadata returns avatar metadata without touching S3.
func (s *service) GetAvatarMetadata(ctx context.Context, id uuid.UUID) (*models.Avatar, error) {
	// traces
	ctx, span := tracer.Start(ctx, "Service.GetAvatarMetadata",
		trace.WithAttributes(attribute.String("avatar.id", id.String())))
	defer span.End()

	if id == uuid.Nil {
		return nil, fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}

	var avatar *models.Avatar
	if cbErr := s.withDBCB(func() error {
		var err error
		avatar, err = s.storage.GetAvatarByID(ctx, id)
		return err
	}); cbErr != nil {
		if isCBOpen(cbErr) {
			return nil, ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrAvatarNotFound) {
			return nil, ErrAvatarNotFound
		}
		if errors.Is(cbErr, repository.ErrInvalidArgument) {
			return nil, ErrInvalidArgument
		}
		tracing.RecordError(span, cbErr)
		return nil, fmt.Errorf("failed to get avatar metadata: %w", cbErr)
	}
	return avatar, nil
}

// ListUserAvatars returns all active avatars of the user.
func (s *service) ListUserAvatars(ctx context.Context, userID string) ([]models.Avatar, error) {
	// traces
	ctx, span := tracer.Start(ctx, "Service.ListUserAvatars",
		trace.WithAttributes(attribute.String("user.id", userID)))
	defer span.End()

	if userID == "" {
		return nil, fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}

	var avatars []models.Avatar
	if cbErr := s.withDBCB(func() error {
		var err error
		avatars, err = s.storage.ListAvatarsByUserID(ctx, userID)
		return err
	}); cbErr != nil {
		if isCBOpen(cbErr) {
			return nil, ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrInvalidArgument) {
			return nil, ErrInvalidArgument
		}
		tracing.RecordError(span, cbErr)
		return nil, fmt.Errorf("failed to list avatars: %w", cbErr)
	}
	span.SetAttributes(attribute.Int("avatars.count", len(avatars)))
	return avatars, nil
}

// ---------- Delete ----------

// DeleteAvatar soft-deletes an avatar and enqueues async S3 cleanup.
// Only the owner (matching X-User-ID) can delete.
func (s *service) DeleteAvatar(ctx context.Context, id uuid.UUID, userID string) (err error) {
	// traces
	ctx, span := tracer.Start(ctx, "Service.DeleteAvatar",
		trace.WithAttributes(
			attribute.String("avatar.id", id.String()),
			attribute.String("user.id", userID),
		),
	)
	defer span.End()
	// metrics
	defer func() {
		metrics.AvatarDeletesTotal.WithLabelValues(classifyDeleteError(err)).Inc()
	}()

	if id == uuid.Nil {
		return fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}
	if userID == "" {
		return fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}

	var avatar *models.Avatar
	if cbErr := s.withDBCB(func() error {
		var err error
		avatar, err = s.storage.GetAvatarByID(ctx, id)
		return err
	}); cbErr != nil {
		if isCBOpen(cbErr) {
			return ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrAvatarNotFound) {
			return ErrAvatarNotFound
		}
		tracing.RecordError(span, cbErr)
		return fmt.Errorf("failed to fetch avatar: %w", cbErr)
	}
	if avatar.UserID != userID {
		return ErrForbidden
	}

	if cbErr := s.withDBCB(func() error {
		return s.storage.SoftDeleteAvatar(ctx, id, userID)
	}); cbErr != nil {
		if isCBOpen(cbErr) {
			return ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrAvatarNotFound) {
			return ErrAvatarNotFound
		}
		if errors.Is(cbErr, repository.ErrNoChanges) {
			return ErrNoChanges
		}
		tracing.RecordError(span, cbErr)
		return fmt.Errorf("failed to soft-delete avatar: %w", cbErr)
	}

	s.publishDelete(ctx, avatar)
	return nil
}

// DeleteActiveUserAvatar soft-deletes the active avatar of the user (by header).
func (s *service) DeleteActiveUserAvatar(ctx context.Context, userID string) (err error) {
	// traces
	ctx, span := tracer.Start(ctx, "Service.DeleteActiveUserAvatar",
		trace.WithAttributes(
			attribute.String("user.id", userID),
		),
	)
	defer span.End()
	// metrics
	defer func() {
		metrics.AvatarDeletesTotal.WithLabelValues(classifyDeleteError(err)).Inc()
	}()

	if userID == "" {
		return fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}

	var avatar *models.Avatar
	if cbErr := s.withDBCB(func() error {
		var err error
		avatar, err = s.storage.GetActiveAvatarByUserID(ctx, userID)
		return err
	}); cbErr != nil {
		if isCBOpen(cbErr) {
			return ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrAvatarNotFound) {
			return ErrAvatarNotFound
		}
		tracing.RecordError(span, cbErr)
		return fmt.Errorf("failed to fetch active avatar: %w", cbErr)
	}

	if cbErr := s.withDBCB(func() error {
		return s.storage.SoftDeleteActiveUserAvatar(ctx, userID)
	}); cbErr != nil {
		if isCBOpen(cbErr) {
			return ErrServiceUnavailable
		}
		if errors.Is(cbErr, repository.ErrAvatarNotFound) {
			return ErrAvatarNotFound
		}
		if errors.Is(cbErr, repository.ErrNoChanges) {
			return ErrNoChanges
		}
		tracing.RecordError(span, cbErr)
		return fmt.Errorf("failed to soft-delete active avatar: %w", cbErr)
	}
	s.publishDelete(ctx, avatar)
	return nil
}

// ---------- Health ----------

// Health probes all dependencies (DB, S3, broker) and returns an aggregated status.
func (s *service) Health(ctx context.Context) models.HealthStatus {
	// traces
	ctx, span := tracer.Start(ctx, "Service.Health")
	defer span.End()

	details := map[string]models.ComponentHealth{}
	status := "ok"

	probe := func(name string, err error) {
		if err != nil {
			details[name] = models.ComponentHealth{Status: "unavailable", Error: err.Error()}
			status = "degraded"
			return
		}
		details[name] = models.ComponentHealth{Status: "ok"}
	}

	probe("database", s.storage.Ping(ctx))
	probe("s3", s.objects.Ping(ctx))
	probe("broker", s.publisher.Ping(ctx))
	span.SetAttributes(attribute.String("health.status", status))
	return models.HealthStatus{Status: status, Details: details}
}

// Close closes the underlying storage. S3 and broker clients are managed by callers.
func (s *service) Close(ctx context.Context) error {
	return s.storage.Close(ctx)
}

// ---------- helpers ----------

func (s *service) publishDelete(ctx context.Context, avatar *models.Avatar) {
	keys := make([]string, 0, len(avatar.ThumbnailS3Keys)+1)
	keys = append(keys, avatar.S3Key)
	for _, k := range avatar.ThumbnailS3Keys {
		keys = append(keys, k)
	}
	event := models.AvatarDeleteEvent{
		AvatarID: avatar.ID.String(),
		S3Keys:   keys,
	}
	if err := s.publisher.PublishDelete(ctx, event); err != nil {
		s.logger.ErrorContext(ctx, "failed to publish delete event",
			slog.String("avatar_id", avatar.ID.String()),
			slog.Any("error", err))
	}
}

func (s *service) placeholder(ctx context.Context) (*models.Avatar, io.ReadCloser, error) {
	if s.config.DefaultAvatarKey == "" {
		return nil, nil, ErrAvatarNotFound
	}
	rc, err := s.objects.Download(ctx, s.config.DefaultAvatarKey)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download default avatar: %w", err)
	}
	return &models.Avatar{
		FileName: "default.png",
		MimeType: "image/png",
		S3Key:    s.config.DefaultAvatarKey,
	}, rc, nil
}

func (s *service) isAllowedMime(ct string) bool {
	ct = strings.ToLower(strings.TrimSpace(ct))
	if i := strings.Index(ct, ";"); i >= 0 {
		ct = strings.TrimSpace(ct[:i])
	}
	for _, allowed := range s.config.AllowedMimeTypes {
		if ct == allowed {
			return true
		}
	}
	return false
}

func (s *service) buildOriginalKey(userID string, id uuid.UUID, fileName string) string {
	ext := strings.ToLower(path.Ext(fileName))
	return fmt.Sprintf("avatars/%s/%s%s", userID, id.String(), ext)
}
