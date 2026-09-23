// Package server provides HTTP server setup with chi router and lifecycle management.
package server

import (
	"context"
	"io"

	"github.com/google/uuid"
	"github.com/max-marek-projects/avatars-service/internal/models"
)

// Service defines the business logic interface expected by the server.
// Mirrors handlers.Service so server tests can mock it without importing
// test-only code from the handlers package.
//
//go:generate mockery --name=Service --output=. --outpkg=server --filename=mock_service.gen_test.go --with-expecter --structname=MockService
type Service interface {
	UploadAvatar(ctx context.Context, userID string, r io.Reader, fileName, contentType string, size int64) (*models.Avatar, error)
	GetAvatarByID(ctx context.Context, id uuid.UUID) (*models.Avatar, io.ReadCloser, error)
	GetActiveAvatar(ctx context.Context, userID string) (*models.Avatar, io.ReadCloser, error)
	GetAvatarMetadata(ctx context.Context, id uuid.UUID) (*models.Avatar, error)
	ListUserAvatars(ctx context.Context, userID string) ([]models.Avatar, error)
	DeleteAvatar(ctx context.Context, id uuid.UUID, userID string) error
	DeleteActiveUserAvatar(ctx context.Context, userID string) error
	Health(ctx context.Context) models.HealthStatus
	Close(ctx context.Context) error
}
