// Package handlers implements the HTTP/REST delivery layer of avatars-service.
package handlers

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"

	"github.com/max-marek-projects/avatars-service/internal/models"
)

// maxUploadSize is the hard cap for a single uploaded file (10 MiB).
const maxUploadSize = 10 << 20

// Handler holds HTTP-layer dependencies.
type Handler struct {
	service Service
	logger  *slog.Logger
}

// NewHandler builds a Handler with defaults.
func NewHandler(service Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handler{service: service, logger: logger}
}

// Close releases resources owned by the handler (delegates to the service).
func (h *Handler) Close(ctx context.Context) error {
	return h.service.Close(ctx)
}

// ---------- shared response helpers ----------

func writeJSON(w http.ResponseWriter, code int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if payload == nil {
		return
	}
	_ = json.NewEncoder(w).Encode(payload)
}

func writeError(w http.ResponseWriter, code int, msg string) {
	writeJSON(w, code, models.ErrorResponse{Error: msg})
}

func (h *Handler) handleServiceError(w http.ResponseWriter, err error) {
	code, msg := httpStatusFromError(err)
	if code >= http.StatusInternalServerError {
		h.logger.Error("handler: service call failed", slog.Any("error", err))
	}
	if code == http.StatusNoContent {
		w.WriteHeader(http.StatusNoContent)
		return
	}
	writeError(w, code, msg)
}

func setImageHeaders(w http.ResponseWriter, a *models.Avatar) {
	if a == nil {
		return
	}
	w.Header().Set("Content-Type", a.MimeType)
	w.Header().Set("Cache-Control", "public, max-age=86400")
	w.Header().Set("ETag", `"`+a.ID.String()+`"`)
	if a.FileName != "" {
		w.Header().Set("Content-Disposition", `inline; filename="`+a.FileName+`"`)
	}
}

func toMetadataResponse(a *models.Avatar) models.AvatarMetadataResponse {
	resp := models.AvatarMetadataResponse{
		ID:        a.ID.String(),
		UserID:    a.UserID,
		FileName:  a.FileName,
		MimeType:  a.MimeType,
		Size:      a.SizeBytes,
		CreatedAt: a.CreatedAt,
		UpdatedAt: a.UpdatedAt,
	}
	if len(a.ThumbnailS3Keys) > 0 {
		resp.Thumbnails = make([]models.ThumbnailInfo, 0, len(a.ThumbnailS3Keys))
		for size := range a.ThumbnailS3Keys {
			resp.Thumbnails = append(resp.Thumbnails, models.ThumbnailInfo{
				Size: size,
				URL:  "/api/v1/avatars/" + a.ID.String() + "?size=" + size,
			})
		}
	}
	return resp
}
