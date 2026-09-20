package services

import "errors"

var (
	// ErrAvatarNotFound is returned when the requested avatar does not exist.
	ErrAvatarNotFound = errors.New("avatar not found")

	// ErrForbidden is returned when the user tries to modify another user's avatar.
	ErrForbidden = errors.New("operation forbidden")

	// ErrInvalidArgument is returned for bad input.
	ErrInvalidArgument = errors.New("invalid argument received")

	// ErrFileTooLarge is returned when the uploaded file exceeds the limit.
	ErrFileTooLarge = errors.New("file too large")

	// ErrUnsupportedFormat is returned when MIME type is not allowed.
	ErrUnsupportedFormat = errors.New("unsupported file format")

	// ErrAvatarConflict is returned when the same avatar record already exists.
	ErrAvatarConflict = errors.New("avatar already exists")

	// ErrNoChanges is returned when an update/delete affects nothing.
	ErrNoChanges = errors.New("no changes")

	// ErrServiceUnavailable is returned when an upstream component is down.
	ErrServiceUnavailable = errors.New("service unavailable")
)
