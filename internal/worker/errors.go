package worker

import "errors"

var (
	// ErrAvatarNotFound is returned when the avatar row is gone.
	ErrAvatarNotFound = errors.New("avatar not found")

	// ErrS3ObjectNotFound is returned when an S3 object is already gone.
	ErrS3ObjectNotFound = errors.New("s3 object not found")

	// ErrProcessingFailed is returned when the worker exhausts retries.
	ErrProcessingFailed = errors.New("processing failed")
)
