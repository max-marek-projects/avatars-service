package services

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/max-marek-projects/avatars-service/internal/models"
)

// Storage defines the persistence contract for avatar metadata.
// Implementations must be safe for concurrent use.
//
//go:generate mockery --name=Storage --output=. --outpkg=services --filename=mock_storage.gen_test.go --with-expecter --structname=MockStorage
type Storage interface {
	// CreateAvatar inserts a new avatar metadata row.
	CreateAvatar(ctx context.Context, avatar *models.Avatar) error

	// GetAvatarByID returns an avatar by its UUID.
	GetAvatarByID(ctx context.Context, id uuid.UUID) (*models.Avatar, error)

	// GetActiveAvatarByUserID returns the most recent non-deleted avatar
	// of the user (used for GET /api/v1/users/{user_id}/avatar).
	GetActiveAvatarByUserID(ctx context.Context, userID string) (*models.Avatar, error)

	// ListAvatarsByUserID returns all non-deleted avatars of the user.
	ListAvatarsByUserID(ctx context.Context, userID string) ([]models.Avatar, error)

	// UpdateUploadStatus updates the upload_status column.
	UpdateUploadStatus(ctx context.Context, id uuid.UUID, status models.UploadStatus) error

	// UpdateProcessingStatus updates processing_status and (optionally) thumbnails.
	UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status models.ProcessingStatus, thumbnails map[string]string) error

	// SoftDeleteAvatar marks the avatar as deleted (sets deleted_at).
	// If userID is not empty, ownership is verified.
	SoftDeleteAvatar(ctx context.Context, id uuid.UUID, userID string) error

	// SoftDeleteActiveUserAvatar soft-deletes the active avatar of the user.
	SoftDeleteActiveUserAvatar(ctx context.Context, userID string) error

	// Ping checks the DB connection (used by /health).
	Ping(ctx context.Context) error

	// Close releases the underlying connection pool.
	Close(ctx context.Context) error
}

// ObjectStorage defines operations against S3/MinIO.
//
//go:generate mockery --name=ObjectStorage --output=. --outpkg=services --filename=mock_object_storage.gen_test.go --with-expecter --structname=MockObjectStorage
type ObjectStorage interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
	Ping(ctx context.Context) error
}

// Publisher defines operations against the message broker.
//
//go:generate mockery --name=Publisher --output=. --outpkg=services --filename=mock_publisher.gen_test.go --with-expecter --structname=MockPublisher
type Publisher interface {
	PublishUpload(ctx context.Context, event models.AvatarUploadEvent) error
	PublishDelete(ctx context.Context, event models.AvatarDeleteEvent) error
	Ping(ctx context.Context) error
}
