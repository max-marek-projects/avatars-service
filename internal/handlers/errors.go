package handlers

import (
	"errors"
	"net/http"

	"github.com/max-marek-projects/avatars-service/internal/services"
)

// httpStatusFromError maps service-level sentinel errors to HTTP status codes.
func httpStatusFromError(err error) (int, string) {
	switch {
	case err == nil:
		return http.StatusOK, ""
	case errors.Is(err, services.ErrInvalidArgument):
		return http.StatusBadRequest, "invalid argument"
	case errors.Is(err, services.ErrUnsupportedFormat):
		return http.StatusBadRequest, "unsupported file format"
	case errors.Is(err, services.ErrFileTooLarge):
		return http.StatusRequestEntityTooLarge, "file too large"
	case errors.Is(err, services.ErrAvatarNotFound):
		return http.StatusNotFound, "avatar not found"
	case errors.Is(err, services.ErrForbidden):
		return http.StatusForbidden, "forbidden"
	case errors.Is(err, services.ErrAvatarConflict):
		return http.StatusConflict, "avatar already exists"
	case errors.Is(err, services.ErrNoChanges):
		return http.StatusNoContent, ""
	case errors.Is(err, services.ErrServiceUnavailable):
		return http.StatusServiceUnavailable, "service unavailable"
	default:
		return http.StatusInternalServerError, "internal error"
	}
}
