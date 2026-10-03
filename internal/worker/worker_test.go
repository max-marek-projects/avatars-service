package worker

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/max-marek-projects/avatars-service/internal/repository"
)

// ---------- helpers ----------

func newTestWorker(t *testing.T, cfg Config) (*Worker, *MockStorage, *MockObjectStorage, *MockConsumer) {
	t.Helper()
	st := NewMockStorage(t)
	obj := NewMockObjectStorage(t)
	cons := NewMockConsumer(t)
	w, err := NewWorker(st, obj, cons, cfg, slog.Default())
	require.NoError(t, err)
	return w, st, obj, cons
}

// makePNG produces a small valid PNG for resize tests.
func makePNG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for x := 0; x < w; x++ {
		for y := 0; y < h; y++ {
			img.Set(x, y, color.RGBA{R: 100, G: 150, B: 200, A: 255})
		}
	}
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, img))
	return buf.Bytes()
}

// ---------- NewWorker ----------

func TestNewWorker(t *testing.T) {
	t.Run("defaults", func(t *testing.T) {
		w, _, _, _ := newTestWorker(t, Config{})
		assert.Equal(t, []int{100, 300}, w.cfg.ThumbnailSizes)
		assert.Equal(t, 3, w.cfg.MaxRetries)
		assert.Equal(t, 500*time.Millisecond, w.cfg.RetryBaseDelay)
	})

	t.Run("nil storage", func(t *testing.T) {
		_, err := NewWorker(nil, NewMockObjectStorage(t), NewMockConsumer(t), Config{}, nil)
		assert.Error(t, err)
	})
	t.Run("nil objects", func(t *testing.T) {
		_, err := NewWorker(NewMockStorage(t), nil, NewMockConsumer(t), Config{}, nil)
		assert.Error(t, err)
	})
	t.Run("nil consumer", func(t *testing.T) {
		_, err := NewWorker(NewMockStorage(t), NewMockObjectStorage(t), nil, Config{}, nil)
		assert.Error(t, err)
	})
}

// ---------- handleUpload ----------

func TestWorker_handleUpload(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	ev := models.AvatarUploadEvent{AvatarID: id.String(), UserID: "u1", S3Key: "avatars/u1/x.png"}
	pngData := makePNG(t, 400, 200)

	t.Run("success", func(t *testing.T) {
		w, st, obj, _ := newTestWorker(t, Config{ThumbnailSizes: []int{100, 300}})

		st.On("GetAvatarByID", ctx, id).Return(&models.Avatar{
			ID: id, UserID: "u1", S3Key: ev.S3Key,
			ProcessingStatus: models.ProcessingStatusPending,
		}, nil)
		st.On("UpdateProcessingStatus", ctx, id, models.ProcessingStatusProcessing, mock.Anything).Return(nil)

		obj.On("Download", ctx, ev.S3Key).Return(io.NopCloser(bytes.NewReader(pngData)), nil)
		obj.On("Upload", ctx, mock.MatchedBy(func(k string) bool {
			return strings.Contains(k, "100x100")
		}), mock.Anything, mock.AnythingOfType("int64"), "image/jpeg").Return(nil)
		obj.On("Upload", ctx, mock.MatchedBy(func(k string) bool {
			return strings.Contains(k, "300x300")
		}), mock.Anything, mock.AnythingOfType("int64"), "image/jpeg").Return(nil)

		st.On("UpdateProcessingStatus", ctx, id, models.ProcessingStatusCompleted,
			mock.MatchedBy(func(m map[string]string) bool {
				return len(m) == 2 && m["100x100"] != "" && m["300x300"] != ""
			})).Return(nil)

		require.NoError(t, w.handleUpload(ctx, ev))
		st.AssertExpectations(t)
		obj.AssertExpectations(t)
	})

	t.Run("already completed — skip", func(t *testing.T) {
		w, st, _, _ := newTestWorker(t, Config{})
		st.On("GetAvatarByID", ctx, id).Return(&models.Avatar{
			ID: id, ProcessingStatus: models.ProcessingStatusCompleted,
		}, nil)
		require.NoError(t, w.handleUpload(ctx, ev))
	})

	t.Run("avatar not found", func(t *testing.T) {
		w, st, _, _ := newTestWorker(t, Config{})
		st.On("GetAvatarByID", ctx, id).Return(nil, repository.ErrAvatarNotFound)
		err := w.handleUpload(ctx, ev)
		assert.ErrorIs(t, err, repository.ErrAvatarNotFound)
	})

	t.Run("empty event", func(t *testing.T) {
		w, _, _, _ := newTestWorker(t, Config{})
		assert.Error(t, w.handleUpload(ctx, models.AvatarUploadEvent{}))
	})

	t.Run("invalid uuid", func(t *testing.T) {
		w, _, _, _ := newTestWorker(t, Config{})
		bad := models.AvatarUploadEvent{AvatarID: "not-uuid", S3Key: "k"}
		assert.Error(t, w.handleUpload(ctx, bad))
	})

	t.Run("download error", func(t *testing.T) {
		w, st, obj, _ := newTestWorker(t, Config{})
		st.On("GetAvatarByID", ctx, id).Return(&models.Avatar{
			ID: id, ProcessingStatus: models.ProcessingStatusPending,
		}, nil)
		st.On("UpdateProcessingStatus", ctx, id, models.ProcessingStatusProcessing, mock.Anything).Return(nil)
		obj.On("Download", ctx, ev.S3Key).Return(nil, errors.New("s3 down"))

		err := w.handleUpload(ctx, ev)
		assert.ErrorContains(t, err, "download original")
	})
}

// ---------- handleUploadWithRetry ----------

func TestWorker_handleUploadWithRetry(t *testing.T) {
	ctx := context.Background()
	id := uuid.New()
	ev := models.AvatarUploadEvent{AvatarID: id.String(), S3Key: "k"}

	t.Run("gives up and marks failed", func(t *testing.T) {
		w, st, obj, _ := newTestWorker(t, Config{
			MaxRetries:     2,
			RetryBaseDelay: time.Millisecond,
		})
		st.On("GetAvatarByID", ctx, id).Return(&models.Avatar{
			ID: id, ProcessingStatus: models.ProcessingStatusPending,
		}, nil)
		st.On("UpdateProcessingStatus", ctx, id, models.ProcessingStatusProcessing, mock.Anything).Return(nil).Maybe()
		obj.On("Download", ctx, "k").Return(nil, errors.New("boom")).Maybe()
		st.On("UpdateProcessingStatus", ctx, id, models.ProcessingStatusFailed, mock.Anything).Return(nil)

		err := w.handleUploadWithRetry(ctx, ev)
		assert.ErrorIs(t, err, ErrProcessingFailed)
	})
}

// ---------- handleDelete ----------

func TestWorker_handleDelete(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		w, _, obj, _ := newTestWorker(t, Config{})
		obj.On("Delete", ctx, "a").Return(nil)
		obj.On("Delete", ctx, "b").Return(nil)
		require.NoError(t, w.handleDelete(ctx, models.AvatarDeleteEvent{
			AvatarID: "x", S3Keys: []string{"a", "b"},
		}))
		obj.AssertExpectations(t)
	})

	t.Run("empty keys — no-op", func(t *testing.T) {
		w, _, _, _ := newTestWorker(t, Config{})
		require.NoError(t, w.handleDelete(ctx, models.AvatarDeleteEvent{AvatarID: "x"}))
	})

	t.Run("empty avatar id", func(t *testing.T) {
		w, _, _, _ := newTestWorker(t, Config{})
		assert.Error(t, w.handleDelete(ctx, models.AvatarDeleteEvent{}))
	})

	t.Run("not found is ok", func(t *testing.T) {
		w, _, obj, _ := newTestWorker(t, Config{})
		obj.On("Delete", ctx, "gone").Return(errors.New("NoSuchKey: gone"))
		require.NoError(t, w.handleDelete(ctx, models.AvatarDeleteEvent{
			AvatarID: "x", S3Keys: []string{"gone"},
		}))
	})

	t.Run("other error", func(t *testing.T) {
		w, _, obj, _ := newTestWorker(t, Config{})
		obj.On("Delete", ctx, "k").Return(errors.New("network down"))
		err := w.handleDelete(ctx, models.AvatarDeleteEvent{
			AvatarID: "x", S3Keys: []string{"k"},
		})
		assert.Error(t, err)
	})
}

// ---------- Run / Ping / Close ----------

func TestWorker_Run_Cancel(t *testing.T) {
	w, _, _, cons := newTestWorker(t, Config{})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var started sync.WaitGroup
	started.Add(2)

	cons.On("ConsumeUpload", mock.Anything, mock.Anything).
		Return(nil).
		Run(func(mock.Arguments) { started.Done() })

	cons.On("ConsumeDelete", mock.Anything, mock.Anything).
		Return(nil).
		Run(func(mock.Arguments) { started.Done() })

	done := make(chan error, 1)
	go func() { done <- w.Run(ctx) }()

	started.Wait()
	cancel()

	select {
	case err := <-done:
		assert.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("Run did not return after context cancellation")
	}
}

func TestWorker_PingAndClose(t *testing.T) {
	w, _, _, cons := newTestWorker(t, Config{})
	ctx := context.Background()

	cons.On("Ping", ctx).Return(nil)
	require.NoError(t, w.Ping(ctx))

	cons.On("Close").Return(nil)
	require.NoError(t, w.Close())
	_ = ctx
}
