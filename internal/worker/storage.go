package worker

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/max-marek-projects/avatars-service/internal/models"
)

// Storage defines the persistence contract for avatar metadata.
// Implementations must be safe for concurrent use.
//
//go:generate mockery --name=Storage --output=. --outpkg=worker --filename=mock_storage.gen_test.go --with-expecter --structname=MockStorage
type Storage interface {
	GetAvatarByID(ctx context.Context, id uuid.UUID) (*models.Avatar, error)
	UpdateProcessingStatus(ctx context.Context, id uuid.UUID, status models.ProcessingStatus, thumbnails map[string]string) error
	UpdateUploadStatus(ctx context.Context, id uuid.UUID, status models.UploadStatus) error
}

// ObjectStorage defines operations against S3/MinIO.
//
//go:generate mockery --name=ObjectStorage --output=. --outpkg=worker --filename=mock_object_storage.gen_test.go --with-expecter --structname=MockObjectStorage
type ObjectStorage interface {
	Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error
	Download(ctx context.Context, key string) (io.ReadCloser, error)
	Delete(ctx context.Context, key string) error
}

// Publisher defines operations against the message broker.
//
//go:generate mockery --name=Publisher --output=. --outpkg=worker --filename=mock_publisher.gen_test.go --with-expecter --structname=MockPublisher
type Publisher interface {
	PublishUpload(ctx context.Context, event models.AvatarUploadEvent) error
	PublishDelete(ctx context.Context, event models.AvatarDeleteEvent) error
	Ping(ctx context.Context) error
}

// Consumer is the broker contract required by the worker.
//
//go:generate mockery --name=Consumer --output=. --outpkg=worker --filename=mock_consumer.gen_test.go --with-expecter --structname=MockConsumer
type Consumer interface {
	ConsumeUpload(ctx context.Context, handler func(context.Context, models.AvatarUploadEvent) error) error
	ConsumeDelete(ctx context.Context, handler func(context.Context, models.AvatarDeleteEvent) error) error
	Ping(ctx context.Context) error
	Close() error
}
