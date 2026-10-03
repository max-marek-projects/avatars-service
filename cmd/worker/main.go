// Package main is the entry point for the avatars-service worker process.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/max-marek-projects/avatars-service/internal/broker"
	"github.com/max-marek-projects/avatars-service/internal/config"
	"github.com/max-marek-projects/avatars-service/internal/logger"
	"github.com/max-marek-projects/avatars-service/internal/repository"
	"github.com/max-marek-projects/avatars-service/internal/storage"
	"github.com/max-marek-projects/avatars-service/internal/worker"
)

var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

func run() error {
	fmt.Println("Worker build version:", orNA(buildVersion))
	fmt.Println("Worker build date:", orNA(buildDate))
	fmt.Println("Worker build commit:", orNA(buildCommit))

	cfg, err := config.LoadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	log, err := logger.New(cfg.LoggerLevel)
	if err != nil {
		return fmt.Errorf("init logger: %w", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Storage: PostgreSQL.
	store, err := repository.NewDBStorage(ctx, cfg.DatabaseURI, cfg.ForceMigrations, log)
	if err != nil {
		return fmt.Errorf("db storage: %w", err)
	}
	defer func() { _ = store.Close(context.Background()) }()

	// Object storage: S3/MinIO.
	s3, err := storage.NewS3Storage(ctx, storage.S3Config{
		Endpoint:  cfg.S3Endpoint,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
		Bucket:    cfg.S3Bucket,
		UseSSL:    cfg.S3UseSSL,
		Region:    cfg.S3Region,
	}, log)
	if err != nil {
		return fmt.Errorf("s3 storage: %w", err)
	}

	// Consumer: RabbitMQ.
	consumer, err := broker.NewRabbitConsumer(broker.RabbitConfig{
		URL:          cfg.RabbitURL,
		ExchangeName: cfg.RabbitExchange,
		UploadKey:    cfg.RabbitUploadKey,
		DeleteKey:    cfg.RabbitDeleteKey,
	}, log)
	if err != nil {
		return fmt.Errorf("rabbit consumer: %w", err)
	}
	defer func() { _ = consumer.Close() }()

	// Worker.
	w, err := worker.NewWorker(store, s3, consumer, worker.Config{
		ThumbnailSizes: []int{100, 300},
		MaxRetries:     3,
		RetryBaseDelay: 500 * time.Millisecond,
	}, log)
	if err != nil {
		return fmt.Errorf("worker: %w", err)
	}
	defer w.Close()

	// Signal handling + run.
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(stop)

	runCtx, runCancel := context.WithCancel(ctx)
	defer runCancel()

	errCh := make(chan error, 1)
	go func() { errCh <- w.Run(runCtx) }()

	select {
	case sig := <-stop:
		log.Info("worker: shutdown signal", slog.String("signal", sig.String()))
		runCancel()
		// give in-flight messages a short grace window
		time.Sleep(200 * time.Millisecond)
	case err := <-errCh:
		if err != nil {
			return fmt.Errorf("worker stopped: %w", err)
		}
	}
	log.Info("worker stopped gracefully")
	return nil
}

func orNA(s string) string {
	if s == "" {
		return "N/A"
	}
	return s
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
}
