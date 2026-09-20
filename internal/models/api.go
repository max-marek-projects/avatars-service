package models

import "time"

// UploadAvatarResponse is returned by POST /api/v1/avatars on success.
type UploadAvatarResponse struct {
	ID        string    `json:"id"`
	UserID    string    `json:"user_id"`
	URL       string    `json:"url"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

// ThumbnailInfo describes one thumbnail in the metadata response.
type ThumbnailInfo struct {
	Size string `json:"size"`
	URL  string `json:"url"`
}

// AvatarMetadataResponse is the JSON representation of an avatar's metadata.
type AvatarMetadataResponse struct {
	ID         string          `json:"id"`
	UserID     string          `json:"user_id"`
	FileName   string          `json:"file_name"`
	MimeType   string          `json:"mime_type"`
	Size       int64           `json:"size"`
	Dimensions *Dimensions     `json:"dimensions,omitempty"`
	Thumbnails []ThumbnailInfo `json:"thumbnails,omitempty"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
}

// Dimensions is reserved for future use.
type Dimensions struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

// ErrorResponse is a uniform API error payload.
type ErrorResponse struct {
	Error   string `json:"error"`
	Details string `json:"details,omitempty"`
}

// HealthStatus is returned by Health and rendered as JSON by the handler.
type HealthStatus struct {
	Status  string                     `json:"status"`
	Details map[string]ComponentHealth `json:"details,omitempty"`
}

// ComponentHealth describes a single dependency.
type ComponentHealth struct {
	Status string `json:"status"`
	Error  string `json:"error,omitempty"`
}
