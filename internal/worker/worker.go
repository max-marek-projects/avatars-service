// Package worker consumes avatar events from the broker and performs
// asynchronous image processing: thumbnail generation and S3 cleanup.
package worker

import (
	"context"
	"fmt"
	"log/slog"
	"time"
)

// Config tunes worker behavior.
type Config struct {
	// ThumbnailSizes is the list of square thumbnail edge lengths.
	ThumbnailSizes []int

	// MaxRetries is the number of attempts per message before giving up.
	MaxRetries int

	// RetryBaseDelay is the initial delay between retries (doubled each attempt).
	RetryBaseDelay time.Duration
}

// Worker processes avatar events asynchronously.
type Worker struct {
	storage  Storage
	objects  ObjectStorage
	consumer Consumer
	logger   *slog.Logger
	cfg      Config
}

// NewWorker builds a Worker with default configuration.
func NewWorker(
	storage Storage,
	objects ObjectStorage,
	consumer Consumer,
	cfg Config,
	logger *slog.Logger,
) (*Worker, error) {
	if storage == nil {
		return nil, fmt.Errorf("nil storage")
	}
	if objects == nil {
		return nil, fmt.Errorf("nil object storage")
	}
	if consumer == nil {
		return nil, fmt.Errorf("nil consumer")
	}
	if logger == nil {
		logger = slog.Default()
	}
	if len(cfg.ThumbnailSizes) == 0 {
		cfg.ThumbnailSizes = []int{100, 300}
	}
	if cfg.MaxRetries <= 0 {
		cfg.MaxRetries = 3
	}
	if cfg.RetryBaseDelay <= 0 {
		cfg.RetryBaseDelay = 500 * time.Millisecond
	}
	return &Worker{
		storage:  storage,
		objects:  objects,
		consumer: consumer,
		logger:   logger,
		cfg:      cfg,
	}, nil
}

// Run blocks until ctx is cancelled or a consumer fails fatally.
// It spawns two goroutines: upload and delete handlers.
func (w *Worker) Run(ctx context.Context) error {
	errCh := make(chan error, 2)

	go func() {
		w.logger.Info("worker: consuming upload events")
		errCh <- w.consumer.ConsumeUpload(ctx, w.handleUploadWithRetry)
	}()
	go func() {
		w.logger.Info("worker: consuming delete events")
		errCh <- w.consumer.ConsumeDelete(ctx, w.handleDeleteWithRetry)
	}()

	select {
	case <-ctx.Done():
		w.logger.Info("worker: context cancelled, draining")
		return nil
	case err := <-errCh:
		return err
	}
}

// Ping reports the broker health (used by /health).
func (w *Worker) Ping(ctx context.Context) error {
	return w.consumer.Ping(ctx)
}

// Close releases broker resources.
func (w *Worker) Close() error {
	return w.consumer.Close()
}
