// Package storage provides an S3-compatible implementation of services.ObjectStorage.
package storage

import (
	"context"
	"fmt"
	"io"
	"log/slog"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config holds connection parameters for the S3/MinIO backend.
type S3Config struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
	Region    string
}

// S3Storage is a thin wrapper around the minio client.
type S3Storage struct {
	client *minio.Client
	bucket string
	logger *slog.Logger
}

// NewS3Storage connects to the endpoint and ensures the bucket exists.
func NewS3Storage(ctx context.Context, cfg S3Config, logger *slog.Logger) (*S3Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, fmt.Errorf("minio client: %w", err)
	}

	exists, err := client.BucketExists(ctx, cfg.Bucket)
	if err != nil {
		return nil, fmt.Errorf("bucket check: %w", err)
	}
	if !exists {
		if err := client.MakeBucket(ctx, cfg.Bucket, minio.MakeBucketOptions{Region: cfg.Region}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
		logger.Info("created S3 bucket", slog.String("bucket", cfg.Bucket))
	}

	return &S3Storage{client: client, bucket: cfg.Bucket, logger: logger}, nil
}

// Upload streams the object to S3. Size must be exact; use -1 only if you
// set the object via PutObjectOptions with a PartSize.
func (s *S3Storage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.client.PutObject(ctx, s.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	return nil
}

// Download returns a stream for the object.
func (s *S3Storage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	// Force a stat to surface NotFound immediately.
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("s3 stat %s: %w", key, err)
	}
	return obj, nil
}

// Delete removes the object. Idempotent: deleting a missing key succeeds.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return fmt.Errorf("s3 delete %s: %w", key, err)
	}
	return nil
}

// Ping checks bucket access via BucketExists.
func (s *S3Storage) Ping(ctx context.Context) error {
	_, err := s.client.BucketExists(ctx, s.bucket)
	return err
}
