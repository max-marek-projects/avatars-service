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

	"github.com/google/uuid"
	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/max-marek-projects/avatars-service/internal/repository"
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
	return &service{
		storage:   storage,
		objects:   objects,
		publisher: publisher,
		logger:    logger,
		config:    cfg,
	}, nil
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
) (*models.Avatar, error) {
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

	// 1. Upload original to S3.
	if err := s.objects.Upload(ctx, key, r, size, contentType); err != nil {
		return nil, fmt.Errorf("failed to upload file to S3: %w", err)
	}

	// 2. Persist metadata.
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
	if err := s.storage.CreateAvatar(ctx, avatar); err != nil {
		// Rollback the S3 upload: we do not want orphan files.
		if delErr := s.objects.Delete(ctx, key); delErr != nil {
			s.logger.Error("failed to rollback S3 upload",
				slog.String("key", key),
				slog.Any("error", delErr))
		}
		if errors.Is(err, repository.ErrAlreadyInStorage) {
			return nil, ErrAvatarConflict
		}
		if errors.Is(err, repository.ErrInvalidArgument) {
			return nil, ErrInvalidArgument
		}
		return nil, fmt.Errorf("failed to store avatar metadata: %w", err)
	}

	// 3. Publish event for the worker.
	event := models.AvatarUploadEvent{
		AvatarID: id.String(),
		UserID:   userID,
		S3Key:    key,
	}
	if err := s.publisher.PublishUpload(ctx, event); err != nil {
		s.logger.Error("failed to publish upload event",
			slog.String("avatar_id", id.String()),
			slog.Any("error", err))
		if updErr := s.storage.UpdateUploadStatus(ctx, id, models.UploadStatusFailed); updErr != nil {
			s.logger.Error("failed to mark avatar as failed",
				slog.String("avatar_id", id.String()),
				slog.Any("error", updErr))
		}
		return nil, fmt.Errorf("failed to enqueue avatar for processing: %w", err)
	}
	return avatar, nil
}

// ---------- Read ----------

// GetAvatarByID returns the avatar and a streaming reader for the original file.
func (s *service) GetAvatarByID(ctx context.Context, id uuid.UUID) (*models.Avatar, io.ReadCloser, error) {
	if id == uuid.Nil {
		return nil, nil, fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}
	avatar, err := s.storage.GetAvatarByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			return nil, nil, ErrAvatarNotFound
		}
		if errors.Is(err, repository.ErrInvalidArgument) {
			return nil, nil, ErrInvalidArgument
		}
		return nil, nil, fmt.Errorf("failed to get avatar: %w", err)
	}
	rc, err := s.objects.Download(ctx, avatar.S3Key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download avatar: %w", err)
	}
	return avatar, rc, nil
}

// GetActiveAvatar returns the latest avatar of the user. When the user
// has no uploaded avatar and DefaultAvatarKey is configured, the placeholder
// is returned instead (this is the main avatars-service use case).
func (s *service) GetActiveAvatar(ctx context.Context, userID string) (*models.Avatar, io.ReadCloser, error) {
	if userID == "" {
		return nil, nil, fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}
	avatar, err := s.storage.GetActiveAvatarByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			return s.placeholder(ctx)
		}
		if errors.Is(err, repository.ErrInvalidArgument) {
			return nil, nil, ErrInvalidArgument
		}
		return nil, nil, fmt.Errorf("failed to get active avatar: %w", err)
	}
	rc, err := s.objects.Download(ctx, avatar.S3Key)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to download avatar: %w", err)
	}
	return avatar, rc, nil
}

// GetAvatarMetadata returns avatar metadata without touching S3.
func (s *service) GetAvatarMetadata(ctx context.Context, id uuid.UUID) (*models.Avatar, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}
	avatar, err := s.storage.GetAvatarByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			return nil, ErrAvatarNotFound
		}
		if errors.Is(err, repository.ErrInvalidArgument) {
			return nil, ErrInvalidArgument
		}
		return nil, fmt.Errorf("failed to get avatar metadata: %w", err)
	}
	return avatar, nil
}

// ListUserAvatars returns all active avatars of the user.
func (s *service) ListUserAvatars(ctx context.Context, userID string) ([]models.Avatar, error) {
	if userID == "" {
		return nil, fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}
	avatars, err := s.storage.ListAvatarsByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrInvalidArgument) {
			return nil, ErrInvalidArgument
		}
		return nil, fmt.Errorf("failed to list avatars: %w", err)
	}
	return avatars, nil
}

// ---------- Delete ----------

// DeleteAvatar soft-deletes an avatar and enqueues async S3 cleanup.
// Only the owner (matching X-User-ID) can delete.
func (s *service) DeleteAvatar(ctx context.Context, id uuid.UUID, userID string) error {
	if id == uuid.Nil {
		return fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}
	if userID == "" {
		return fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}

	avatar, err := s.storage.GetAvatarByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			return ErrAvatarNotFound
		}
		return fmt.Errorf("failed to fetch avatar: %w", err)
	}
	if avatar.UserID != userID {
		return ErrForbidden
	}

	if err := s.storage.SoftDeleteAvatar(ctx, id, userID); err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			return ErrAvatarNotFound
		}
		if errors.Is(err, repository.ErrNoChanges) {
			return ErrNoChanges
		}
		return fmt.Errorf("failed to soft-delete avatar: %w", err)
	}

	s.publishDelete(ctx, avatar)
	return nil
}

// DeleteActiveUserAvatar soft-deletes the active avatar of the user (by header).
func (s *service) DeleteActiveUserAvatar(ctx context.Context, userID string) error {
	if userID == "" {
		return fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}
	avatar, err := s.storage.GetActiveAvatarByUserID(ctx, userID)
	if err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			return ErrAvatarNotFound
		}
		return fmt.Errorf("failed to fetch active avatar: %w", err)
	}
	if err := s.storage.SoftDeleteActiveUserAvatar(ctx, userID); err != nil {
		if errors.Is(err, repository.ErrAvatarNotFound) {
			return ErrAvatarNotFound
		}
		if errors.Is(err, repository.ErrNoChanges) {
			return ErrNoChanges
		}
		return fmt.Errorf("failed to soft-delete active avatar: %w", err)
	}
	s.publishDelete(ctx, avatar)
	return nil
}

// ---------- Health ----------

// Health probes all dependencies (DB, S3, broker) and returns an aggregated status.
func (s *service) Health(ctx context.Context) models.HealthStatus {
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
		s.logger.Error("failed to publish delete event",
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
