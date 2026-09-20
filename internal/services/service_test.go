package services

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/max-marek-projects/avatars-service/internal/repository"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func newTestService(t *testing.T, cfg Config) (*service, *MockStorage, *MockObjectStorage, *MockPublisher) {
	t.Helper()
	st := NewMockStorage(t)
	obj := NewMockObjectStorage(t)
	pub := NewMockPublisher(t)
	s, err := NewService(st, obj, pub, cfg, slog.Default())
	require.NoError(t, err)
	return s, st, obj, pub
}

func TestNewService(t *testing.T) {
	t.Run("ok with defaults", func(t *testing.T) {
		s, _, _, _ := newTestService(t, Config{})
		assert.Equal(t, int64(10*1024*1024), s.config.MaxFileSize)
		assert.Contains(t, s.config.AllowedMimeTypes, "image/png")
	})

	t.Run("nil storage", func(t *testing.T) {
		_, err := NewService(nil, NewMockObjectStorage(t), NewMockPublisher(t), Config{}, nil)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
	t.Run("nil object storage", func(t *testing.T) {
		_, err := NewService(NewMockStorage(t), nil, NewMockPublisher(t), Config{}, nil)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
	t.Run("nil publisher", func(t *testing.T) {
		_, err := NewService(NewMockStorage(t), NewMockObjectStorage(t), nil, Config{}, nil)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestService_UploadAvatar(t *testing.T) {
	ctx := context.Background()
	body := strings.NewReader("image-bytes")
	const (
		userID = "user-1"
		file   = "avatar.png"
		mime   = "image/png"
		size   = int64(11)
	)

	t.Run("success", func(t *testing.T) {
		s, st, obj, pub := newTestService(t, Config{})

		obj.On("Upload", ctx, mock.AnythingOfType("string"), body, size, mime).Return(nil)
		st.On("CreateAvatar", ctx, mock.MatchedBy(func(a *models.Avatar) bool {
			return a.UserID == userID && a.S3Key != "" && a.ProcessingStatus == models.ProcessingStatusPending
		})).Return(nil)
		pub.On("PublishUpload", ctx, mock.AnythingOfType("models.AvatarUploadEvent")).Return(nil)

		av, err := s.UploadAvatar(ctx, userID, body, file, mime, size)
		require.NoError(t, err)
		assert.Equal(t, userID, av.UserID)
		assert.Equal(t, mime, av.MimeType)
		st.AssertExpectations(t)
		obj.AssertExpectations(t)
		pub.AssertExpectations(t)
	})

	t.Run("empty user id", func(t *testing.T) {
		s, _, _, _ := newTestService(t, Config{})
		_, err := s.UploadAvatar(ctx, "", body, file, mime, size)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})

	t.Run("empty file", func(t *testing.T) {
		s, _, _, _ := newTestService(t, Config{})
		_, err := s.UploadAvatar(ctx, userID, body, file, mime, 0)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})

	t.Run("file too large", func(t *testing.T) {
		s, _, _, _ := newTestService(t, Config{MaxFileSize: 5})
		_, err := s.UploadAvatar(ctx, userID, body, file, mime, 100)
		assert.ErrorIs(t, err, ErrFileTooLarge)
	})

	t.Run("unsupported mime", func(t *testing.T) {
		s, _, _, _ := newTestService(t, Config{})
		_, err := s.UploadAvatar(ctx, userID, body, file, "application/pdf", size)
		assert.ErrorIs(t, err, ErrUnsupportedFormat)
	})

	t.Run("s3 upload error", func(t *testing.T) {
		s, _, obj, _ := newTestService(t, Config{})
		obj.On("Upload", ctx, mock.Anything, body, size, mime).Return(errors.New("s3 down"))
		_, err := s.UploadAvatar(ctx, userID, body, file, mime, size)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "upload file to S3")
	})

	t.Run("db error rolls back s3", func(t *testing.T) {
		s, st, obj, _ := newTestService(t, Config{})
		obj.On("Upload", ctx, mock.Anything, body, size, mime).Return(nil)
		st.On("CreateAvatar", ctx, mock.Anything).Return(repository.ErrAlreadyInStorage)
		obj.On("Delete", ctx, mock.AnythingOfType("string")).Return(nil)

		_, err := s.UploadAvatar(ctx, userID, body, file, mime, size)
		assert.ErrorIs(t, err, ErrAvatarConflict)
		obj.AssertCalled(t, "Delete", ctx, mock.AnythingOfType("string"))
	})

	t.Run("publish error marks upload failed", func(t *testing.T) {
		s, st, obj, pub := newTestService(t, Config{})
		obj.On("Upload", ctx, mock.Anything, body, size, mime).Return(nil)
		st.On("CreateAvatar", ctx, mock.Anything).Return(nil)
		pub.On("PublishUpload", ctx, mock.Anything).Return(errors.New("broker down"))
		st.On("UpdateUploadStatus", ctx, mock.Anything, models.UploadStatusFailed).Return(nil)

		_, err := s.UploadAvatar(ctx, userID, body, file, mime, size)
		assert.Error(t, err)
		st.AssertCalled(t, "UpdateUploadStatus", ctx, mock.Anything, models.UploadStatusFailed)
	})
}

func TestService_GetAvatarByID(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	rc := io.NopCloser(strings.NewReader("x"))

	t.Run("success", func(t *testing.T) {
		s, st, obj, _ := newTestService(t, Config{})
		av := &models.Avatar{ID: id, S3Key: "k"}
		st.On("GetAvatarByID", ctx, id).Return(av, nil)
		obj.On("Download", ctx, "k").Return(rc, nil)

		got, gotRC, err := s.GetAvatarByID(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, id, got.ID)
		assert.NotNil(t, gotRC)
	})

	t.Run("not found", func(t *testing.T) {
		s, st, _, _ := newTestService(t, Config{})
		st.On("GetAvatarByID", ctx, id).Return(nil, repository.ErrAvatarNotFound)
		_, _, err := s.GetAvatarByID(ctx, id)
		assert.ErrorIs(t, err, ErrAvatarNotFound)
	})

	t.Run("empty id", func(t *testing.T) {
		s, _, _, _ := newTestService(t, Config{})
		_, _, err := s.GetAvatarByID(ctx, uuid.Nil)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestService_GetActiveAvatar_Placeholder(t *testing.T) {
	ctx := context.Background()
	rc := io.NopCloser(strings.NewReader("ph"))

	s, st, obj, _ := newTestService(t, Config{DefaultAvatarKey: "defaults/placeholder.png"})
	st.On("GetActiveAvatarByUserID", ctx, "u1").Return(nil, repository.ErrAvatarNotFound)
	obj.On("Download", ctx, "defaults/placeholder.png").Return(rc, nil)

	av, gotRC, err := s.GetActiveAvatar(ctx, "u1")
	require.NoError(t, err)
	assert.Equal(t, "defaults/placeholder.png", av.S3Key)
	assert.NotNil(t, gotRC)
}

func TestService_GetActiveAvatar_NotFoundWithoutDefault(t *testing.T) {
	ctx := context.Background()
	s, st, _, _ := newTestService(t, Config{})
	st.On("GetActiveAvatarByUserID", ctx, "u1").Return(nil, repository.ErrAvatarNotFound)

	_, _, err := s.GetActiveAvatar(ctx, "u1")
	assert.ErrorIs(t, err, ErrAvatarNotFound)
}

func TestService_DeleteAvatar(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	av := &models.Avatar{
		ID:              id,
		UserID:          "u1",
		S3Key:           "originals/x.png",
		ThumbnailS3Keys: map[string]string{"100x100": "t1", "300x300": "t2"},
	}

	t.Run("success", func(t *testing.T) {
		s, st, _, pub := newTestService(t, Config{})
		st.On("GetAvatarByID", ctx, id).Return(av, nil)
		st.On("SoftDeleteAvatar", ctx, id, "u1").Return(nil)
		pub.On("PublishDelete", ctx, mock.MatchedBy(func(e models.AvatarDeleteEvent) bool {
			return e.AvatarID == id.String() && len(e.S3Keys) == 3
		})).Return(nil)

		err := s.DeleteAvatar(ctx, id, "u1")
		assert.NoError(t, err)
		pub.AssertExpectations(t)
	})

	t.Run("forbidden", func(t *testing.T) {
		s, st, _, _ := newTestService(t, Config{})
		st.On("GetAvatarByID", ctx, id).Return(av, nil)
		err := s.DeleteAvatar(ctx, id, "other")
		assert.ErrorIs(t, err, ErrForbidden)
	})

	t.Run("not found", func(t *testing.T) {
		s, st, _, _ := newTestService(t, Config{})
		st.On("GetAvatarByID", ctx, id).Return(nil, repository.ErrAvatarNotFound)
		err := s.DeleteAvatar(ctx, id, "u1")
		assert.ErrorIs(t, err, ErrAvatarNotFound)
	})

	t.Run("publish error does not fail delete", func(t *testing.T) {
		s, st, _, pub := newTestService(t, Config{})
		st.On("GetAvatarByID", ctx, id).Return(av, nil)
		st.On("SoftDeleteAvatar", ctx, id, "u1").Return(nil)
		pub.On("PublishDelete", ctx, mock.Anything).Return(errors.New("broker down"))

		err := s.DeleteAvatar(ctx, id, "u1")
		assert.NoError(t, err) // soft-delete already happened
	})
}

func TestService_ListUserAvatars(t *testing.T) {
	ctx := context.Background()
	s, st, _, _ := newTestService(t, Config{})
	expected := []models.Avatar{{ID: uuid.New(), UserID: "u1"}}
	st.On("ListAvatarsByUserID", ctx, "u1").Return(expected, nil)

	got, err := s.ListUserAvatars(ctx, "u1")
	require.NoError(t, err)
	assert.Equal(t, expected, got)

	_, err = s.ListUserAvatars(ctx, "")
	assert.ErrorIs(t, err, ErrInvalidArgument)
}

func TestService_Health(t *testing.T) {
	ctx := context.Background()

	t.Run("all ok", func(t *testing.T) {
		s, st, obj, pub := newTestService(t, Config{})
		st.On("Ping", ctx).Return(nil)
		obj.On("Ping", ctx).Return(nil)
		pub.On("Ping", ctx).Return(nil)

		h := s.Health(ctx)
		assert.Equal(t, "ok", h.Status)
		assert.Equal(t, "ok", h.Details["database"].Status)
		assert.Equal(t, "ok", h.Details["s3"].Status)
		assert.Equal(t, "ok", h.Details["broker"].Status)
	})

	t.Run("degraded", func(t *testing.T) {
		s, st, obj, pub := newTestService(t, Config{})
		st.On("Ping", ctx).Return(errors.New("db down"))
		obj.On("Ping", ctx).Return(nil)
		pub.On("Ping", ctx).Return(nil)

		h := s.Health(ctx)
		assert.Equal(t, "degraded", h.Status)
		assert.Equal(t, "unavailable", h.Details["database"].Status)
	})
}

func TestService_Close(t *testing.T) {
	ctx := context.Background()
	s, st, _, _ := newTestService(t, Config{})
	st.On("Close", ctx).Return(nil)
	require.NoError(t, s.Close(ctx))
	st.AssertExpectations(t)
}
