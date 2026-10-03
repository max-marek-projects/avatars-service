package repository

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/pashagolub/pgxmock/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var errConnDone = errors.New("connection is done")

func newMockStorage(t *testing.T) (*dbStorage, pgxmock.PgxPoolIface) {
	t.Helper()
	mock, err := pgxmock.NewPool()
	require.NoError(t, err)
	t.Cleanup(mock.Close)
	return &dbStorage{storage: mock}, mock
}

func avatarRows() *pgxmock.Rows {
	return pgxmock.NewRows([]string{
		"id", "user_id", "file_name", "mime_type", "size_bytes",
		"s3_key", "thumbnail_s3_keys", "upload_status", "processing_status",
		"created_at", "updated_at", "deleted_at",
	})
}

func TestDBStorage_CreateAvatar(t *testing.T) {
	ctx := context.Background()
	avatar := &models.Avatar{
		ID:               uuid.New(),
		UserID:           "user-1",
		FileName:         "a.png",
		MimeType:         "image/png",
		SizeBytes:        1024,
		S3Key:            "avatars/user-1/a.png",
		UploadStatus:     "uploaded",
		ProcessingStatus: "pending",
	}

	t.Run("success", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectExec(`INSERT INTO avatars`).
			WithArgs(avatar.ID, avatar.UserID, avatar.FileName, avatar.MimeType,
				avatar.SizeBytes, avatar.S3Key, []byte(nil),
				avatar.UploadStatus, avatar.ProcessingStatus).
			WillReturnResult(pgxmock.NewResult("INSERT", 1))

		err := storage.CreateAvatar(ctx, avatar)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("duplicate returns ErrAlreadyInStorage", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectExec(`INSERT INTO avatars`).
			WithArgs(
				avatar.ID, avatar.UserID, avatar.FileName, avatar.MimeType,
				avatar.SizeBytes, avatar.S3Key, []byte(nil),
				avatar.UploadStatus, avatar.ProcessingStatus,
			).
			WillReturnError(&pgconn.PgError{Code: "23505"})

		err := storage.CreateAvatar(ctx, avatar)
		assert.ErrorIs(t, err, ErrAlreadyInStorage)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("nil avatar", func(t *testing.T) {
		storage, _ := newMockStorage(t)
		err := storage.CreateAvatar(ctx, nil)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})

	t.Run("empty user id", func(t *testing.T) {
		storage, _ := newMockStorage(t)
		err := storage.CreateAvatar(ctx, &models.Avatar{ID: uuid.New()})
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})

	t.Run("empty s3 key", func(t *testing.T) {
		storage, _ := newMockStorage(t)
		err := storage.CreateAvatar(ctx, &models.Avatar{
			ID: uuid.New(), UserID: "u", FileName: "f",
		})
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestDBStorage_GetAvatarByID(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	now := time.Now()

	t.Run("found", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		rows := avatarRows().AddRow(
			id, "user-1", "a.png", "image/png", int64(1024),
			"avatars/user-1/a.png", []byte(`{"100x100":"k1","300x300":"k2"}`),
			"uploaded", "completed", now, now, nil,
		)
		mock.ExpectQuery(`SELECT .* FROM avatars WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(id).
			WillReturnRows(rows)

		got, err := storage.GetAvatarByID(ctx, id)
		require.NoError(t, err)
		assert.Equal(t, id, got.ID)
		assert.Equal(t, "k1", got.ThumbnailS3Keys["100x100"])
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("not found", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectQuery(`SELECT .* FROM avatars WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(id).
			WillReturnError(pgx.ErrNoRows)

		_, err := storage.GetAvatarByID(ctx, id)
		assert.ErrorIs(t, err, ErrAvatarNotFound)
	})

	t.Run("empty id", func(t *testing.T) {
		storage, _ := newMockStorage(t)
		_, err := storage.GetAvatarByID(ctx, uuid.Nil)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestDBStorage_UpdateProcessingStatus(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	t.Run("with thumbnails", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		thumbs := map[string]string{"100x100": "k1", "300x300": "k2"}
		raw, _ := json.Marshal(thumbs)

		mock.ExpectExec(`UPDATE avatars SET processing_status = \$1, thumbnail_s3_keys = \$2`).
			WithArgs(models.ProcessingStatusCompleted, raw, id).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := storage.UpdateProcessingStatus(ctx, id, models.ProcessingStatusCompleted, thumbs)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no rows -> ErrNoChanges", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectExec(`UPDATE avatars SET processing_status = \$1, updated_at = NOW\(\)`).
			WithArgs(models.ProcessingStatusCompleted, id).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := storage.UpdateProcessingStatus(ctx, id, models.ProcessingStatusCompleted, nil)
		assert.ErrorIs(t, err, ErrNoChanges)
	})
}

func TestDBStorage_UpdateUploadStatus(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	t.Run("success", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectExec(`UPDATE avatars SET upload_status = \$1, updated_at = NOW\(\)`).
			WithArgs(string(models.UploadStatusUploaded), id).
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := storage.UpdateUploadStatus(ctx, id, models.UploadStatusUploaded)
		assert.NoError(t, err)
		assert.NoError(t, mock.ExpectationsWereMet())
	})

	t.Run("no rows -> ErrNoChanges", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectExec(`UPDATE avatars SET upload_status = \$1, updated_at = NOW\(\)`).
			WithArgs(string(models.UploadStatusUploaded), id).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := storage.UpdateUploadStatus(ctx, id, models.UploadStatusUploaded)
		assert.ErrorIs(t, err, ErrNoChanges)
	})

	t.Run("db error", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectExec(`UPDATE avatars SET upload_status = \$1, updated_at = NOW\(\)`).
			WithArgs(string(models.UploadStatusUploaded), id).
			WillReturnError(errConnDone)

		err := storage.UpdateUploadStatus(ctx, id, models.UploadStatusUploaded)
		assert.ErrorIs(t, err, errConnDone)
	})

	t.Run("empty id", func(t *testing.T) {
		storage, _ := newMockStorage(t)
		err := storage.UpdateUploadStatus(ctx, uuid.Nil, models.UploadStatusUploaded)
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})

	t.Run("empty status", func(t *testing.T) {
		storage, _ := newMockStorage(t)
		err := storage.UpdateUploadStatus(ctx, id, "")
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestDBStorage_SoftDeleteAvatar(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()

	t.Run("with ownership", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectExec(`UPDATE avatars SET deleted_at = NOW\(\), updated_at = NOW\(\) WHERE id = \$1 AND deleted_at IS NULL AND user_id = \$2`).
			WithArgs(id, "user-1").
			WillReturnResult(pgxmock.NewResult("UPDATE", 1))

		err := storage.SoftDeleteAvatar(ctx, id, "user-1")
		assert.NoError(t, err)
	})

	t.Run("not found", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		mock.ExpectExec(`UPDATE avatars SET deleted_at = NOW\(\), updated_at = NOW\(\) WHERE id = \$1 AND deleted_at IS NULL`).
			WithArgs(id).
			WillReturnResult(pgxmock.NewResult("UPDATE", 0))

		err := storage.SoftDeleteAvatar(ctx, id, "")
		assert.ErrorIs(t, err, ErrAvatarNotFound)
	})
}

func TestDBStorage_ListAvatarsByUserID(t *testing.T) {
	ctx := context.Background()
	now := time.Now()

	t.Run("ok", func(t *testing.T) {
		storage, mock := newMockStorage(t)
		rows := avatarRows().
			AddRow(uuid.New(), "u1", "a.png", "image/png", int64(1),
				"k", []byte(nil), "uploaded", "completed", now, now, nil).
			AddRow(uuid.New(), "u1", "b.png", "image/png", int64(2),
				"k2", []byte(nil), "uploaded", "completed", now, now, nil)

		mock.ExpectQuery(`SELECT .* FROM avatars WHERE user_id = \$1 AND deleted_at IS NULL`).
			WithArgs("u1").
			WillReturnRows(rows)

		got, err := storage.ListAvatarsByUserID(ctx, "u1")
		require.NoError(t, err)
		assert.Len(t, got, 2)
	})

	t.Run("empty user id", func(t *testing.T) {
		storage, _ := newMockStorage(t)
		_, err := storage.ListAvatarsByUserID(ctx, "")
		assert.ErrorIs(t, err, ErrInvalidArgument)
	})
}

func TestDBStorage_Ping(t *testing.T) {
	storage, mock := newMockStorage(t)
	mock.ExpectPing()
	assert.NoError(t, storage.Ping(context.Background()))
}
