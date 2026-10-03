package models

// AvatarUploadEvent is published by the service right after the original
// image has been stored in S3 and metadata has been persisted.
//
// Routing:
//   - RabbitMQ: exchange "avatars.exchange", routing key "avatar.uploaded"
//   - Kafka:    topic "avatar-events", key = AvatarID
type AvatarUploadEvent struct {
	AvatarID string `json:"avatar_id"`
	UserID   string `json:"user_id"`
	S3Key    string `json:"s3_key"`
}

// ProcessingOp is a single operation the worker must perform on an avatar.
// Reserved for future use (e.g. extra sizes, format conversion).
type ProcessingOp struct {
	// Type identifies the operation, e.g. "resize".
	Type string `json:"type"`

	// Params are op-specific parameters, e.g. {"size": "100x100"}.
	Params map[string]string `json:"params,omitempty"`
}

// AvatarProcessEvent is an optional explicit "process this avatar" command.
// The current worker can derive operations from AvatarUploadEvent, but the
// event is kept for compatibility with the spec.
type AvatarProcessEvent struct {
	AvatarID   string         `json:"avatar_id"`
	Operations []ProcessingOp `json:"operations"`
}

// AvatarDeleteEvent is published after a soft-delete so the worker can
// remove the original file and all thumbnails from S3.
//
// Routing:
//   - RabbitMQ: exchange "avatars.exchange", routing key "avatar.deleted"
//   - Kafka:    topic "avatar-events", key = AvatarID
type AvatarDeleteEvent struct {
	AvatarID string   `json:"avatar_id"`
	S3Keys   []string `json:"s3_keys"`
}
