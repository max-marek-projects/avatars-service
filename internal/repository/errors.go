package repository

import "errors"

// ErrAvatarNotFound is returned when an avatar does not exist (or is soft-deleted).
var ErrAvatarNotFound = errors.New("avatar not found")

// ErrAlreadyInStorage is returned when attempting to insert a duplicate avatar.
var ErrAlreadyInStorage = errors.New("avatar already exists in storage")

// ErrInvalidArgument is returned when an invalid argument value was received.
var ErrInvalidArgument = errors.New("invalid argument received")

// ErrNoChanges is returned when no rows were affected by an update/delete.
var ErrNoChanges = errors.New("no changes")

// ErrForbidden is returned when a user tries to modify another user's avatar.
var ErrForbidden = errors.New("operation forbidden for this user")
