// Package main is the entry point for the avatars-service HTTP server.
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
	"github.com/max-marek-projects/avatars-service/internal/handlers"
	"github.com/max-marek-projects/avatars-service/internal/logger"
	"github.com/max-marek-projects/avatars-service/internal/repository"
	"github.com/max-marek-projects/avatars-service/internal/server"
	"github.com/max-marek-projects/avatars-service/internal/services"
	"github.com/max-marek-projects/avatars-service/internal/storage"
)

var (
	buildVersion string
	buildDate    string
	buildCommit  string
)

// run wires all dependencies and runs the HTTP server until a termination
// signal is received.
func run() error {
	fmt.Println("Build version:", orNA(buildVersion))
	fmt.Println("Build date:", orNA(buildDate))
	fmt.Println("Build commit:", orNA(buildCommit))

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

	// 1. PostgreSQL.
	store, err := repository.NewDBStorage(ctx, cfg.DatabaseURI, cfg.ForceMigrations, log)
	if err != nil {
		log.Error("unable to create DB storage", slog.Any("error", err))
		return err
	}
	defer func() {
		if closeErr := store.Close(context.Background()); closeErr != nil {
			log.Warn("failed to close DB", slog.Any("error", closeErr))
		}
	}()

	// 2. S3 / MinIO.
	s3, err := storage.NewS3Storage(ctx, storage.S3Config{
		Endpoint:  cfg.S3Endpoint,
		AccessKey: cfg.S3AccessKey,
		SecretKey: cfg.S3SecretKey,
		Bucket:    cfg.S3Bucket,
		UseSSL:    cfg.S3UseSSL,
		Region:    cfg.S3Region,
	}, log)
	if err != nil {
		log.Error("unable to create S3 storage", slog.Any("error", err))
		return err
	}

	// 3. RabbitMQ publisher.
	pub, err := broker.NewRabbitPublisher(broker.RabbitConfig{
		URL:          cfg.RabbitURL,
		ExchangeName: cfg.RabbitExchange,
		UploadKey:    cfg.RabbitUploadKey,
		DeleteKey:    cfg.RabbitDeleteKey,
	}, log)
	if err != nil {
		log.Error("unable to create RabbitMQ publisher", slog.Any("error", err))
		return err
	}
	defer func() {
		if cancelErr := pub.Close(); cancelErr != nil {
			log.Warn("failed to close RabbitMQ", slog.Any("error", cancelErr))
		}
	}()

	// 4. Business-logic service.
	svc, err := services.NewService(store, s3, pub, services.Config{
		MaxFileSize:      cfg.MaxFileSize,
		AllowedMimeTypes: cfg.AllowedMimeTypes,
		DefaultAvatarKey: cfg.DefaultAvatarKey,
	}, log)
	if err != nil {
		log.Error("unable to create service", slog.Any("error", err))
		return err
	}

	// 5. HTTP handler + server.
	h := handlers.NewHandler(svc, log, cfg.MaxFileSize)
	srv, err := server.NewServer(
		cfg.RunAddr,
		h,
		cfg.ReadTimeout.Duration(),
		cfg.WriteTimeout.Duration(),
		cfg.StaticDir,
		log,
	)
	if err != nil {
		log.Error("unable to initialize HTTP server", slog.Any("error", err))
		return err
	}

	srvErr := make(chan error, 1)
	go func() { srvErr <- srv.ListenAndServe() }()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
	defer signal.Stop(stop)

	select {
	case sig := <-stop:
		log.Info("shutdown signal received", slog.String("signal", sig.String()))
		shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelShutdown()
		if err := srv.Shutdown(shutdownCtx); err != nil {
			log.Error("graceful shutdown failed", slog.Any("error", err))
			return err
		}
		log.Info("HTTP server stopped gracefully")

	case err := <-srvErr:
		if err != nil {
			log.Error("HTTP server stopped with error", slog.Any("error", err))
			return err
		}
	}
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
