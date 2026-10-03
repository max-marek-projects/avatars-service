package services

import (
	"errors"

	"go.opentelemetry.io/otel"
)

// tracer picks up the global TracerProvider installed by tracing.Init.
var tracer = otel.Tracer("avatars-service/internal/services")

// classifyUploadError maps a service error to a stable metric label.
func classifyUploadError(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, ErrFileTooLarge):
		return "too_large"
	case errors.Is(err, ErrUnsupportedFormat):
		return "unsupported_format"
	case errors.Is(err, ErrAvatarConflict):
		return "conflict"
	case errors.Is(err, ErrInvalidArgument):
		return "invalid_argument"
	default:
		return "internal_error"
	}
}

// classifyDeleteError maps a service error to a stable metric label.
func classifyDeleteError(err error) string {
	switch {
	case err == nil:
		return "success"
	case errors.Is(err, ErrAvatarNotFound):
		return "not_found"
	case errors.Is(err, ErrForbidden):
		return "forbidden"
	case errors.Is(err, ErrInvalidArgument):
		return "invalid_argument"
	default:
		return "internal_error"
	}
}
