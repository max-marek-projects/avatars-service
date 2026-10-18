// Package storage provides an S3-compatible implementation of services.ObjectStorage
// using AWS SDK for Go v2 with built-in OpenTelemetry instrumentation.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/aws/smithy-go/metrics/smithyotelmetrics"
	"github.com/aws/smithy-go/tracing/smithyoteltracing"
	"go.opentelemetry.io/otel"
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

// S3Storage is a wrapper around the AWS S3 client.
type S3Storage struct {
	client   *s3.Client
	uploader *transfermanager.Client
	bucket   string
	logger   *slog.Logger
}

// NewS3Storage connects to the endpoint and ensures the bucket exists.
func NewS3Storage(ctx context.Context, cfg S3Config, logger *slog.Logger) (*S3Storage, error) {
	if logger == nil {
		logger = slog.Default()
	}

	scheme := "http"
	if cfg.UseSSL {
		scheme = "https"
	}
	baseEndpoint := fmt.Sprintf("%s://%s", scheme, cfg.Endpoint)

	region := cfg.Region
	if region == "" {
		region = "us-east-1"
	}

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("aws config: %w", err)
	}

	client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(baseEndpoint)
		o.UsePathStyle = true

		o.TracerProvider = smithyoteltracing.Adapt(otel.GetTracerProvider())
		o.MeterProvider = smithyotelmetrics.Adapt(otel.GetMeterProvider())
	})

	_, err = client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(cfg.Bucket)})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			if _, err := client.CreateBucket(ctx, &s3.CreateBucketInput{
				Bucket: aws.String(cfg.Bucket),
			}); err != nil {
				return nil, fmt.Errorf("create bucket: %w", err)
			}
			logger.InfoContext(ctx, "created S3 bucket", slog.String("bucket", cfg.Bucket))
		} else {
			return nil, fmt.Errorf("bucket check: %w", err)
		}
	}

	return &S3Storage{
		client:   client,
		uploader: transfermanager.New(client),
		bucket:   cfg.Bucket,
		logger:   logger,
	}, nil
}

// Upload streams the object to S3.
func (s *S3Storage) Upload(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(key),
		Body:        r,
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("s3 put %s: %w", key, err)
	}
	return nil
}

// Download returns a stream for the object.
func (s *S3Storage) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return nil, fmt.Errorf("s3 get %s: %w", key, err)
	}
	return out.Body, nil
}

// Delete removes the object. Idempotent: deleting a missing key succeeds.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(key),
	}); err != nil {
		return fmt.Errorf("s3 delete %s: %w", key, err)
	}
	return nil
}

// Ping checks bucket access via HeadBucket.
func (s *S3Storage) Ping(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{
		Bucket: aws.String(s.bucket),
	})
	return err
}
