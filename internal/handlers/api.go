package handlers

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/max-marek-projects/avatars-service/internal/models"
)

// Health handles GET /health.
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	status := h.service.Health(r.Context())
	code := http.StatusOK
	if status.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, status)
}

// UploadAvatar handles POST /api/v1/avatars.
func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing X-User-ID header")
		return
	}

	// Enforce the hard cap before touching the body.
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadSize)
	if err := r.ParseMultipartForm(h.maxUploadSize); err != nil {
		h.logger.Warn("handler: multipart parse failed", slog.Any("error", err))
		writeError(w, http.StatusRequestEntityTooLarge, "file too large")
		return
	}

	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "file field is required")
		return
	}
	defer file.Close()

	ct := header.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/octet-stream"
	}

	avatar, err := h.service.UploadAvatar(r.Context(), userID, file, header.Filename, ct, header.Size)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusCreated, models.UploadAvatarResponse{
		ID:        avatar.ID.String(),
		UserID:    avatar.UserID,
		URL:       fmt.Sprintf("/api/v1/avatars/%s", avatar.ID),
		Status:    string(avatar.ProcessingStatus),
		CreatedAt: avatar.CreatedAt,
	})
}

// GetAvatarByID handles GET /api/v1/avatars/{avatar_id}.
func (h *Handler) GetAvatarByID(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "avatar_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid avatar id")
		return
	}

	avatar, rc, err := h.service.GetAvatarByID(r.Context(), id)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	defer rc.Close()

	// Resizing on the fly is a
	// follow-up. Reserved for future use.
	_ = r.URL.Query().Get("size")
	_ = r.URL.Query().Get("format")

	setImageHeaders(w, avatar)
	if _, err := io.Copy(w, rc); err != nil {
		h.logger.Error("handler: stream avatar failed", slog.Any("error", err))
	}
}

// GetActiveAvatar handles GET /api/v1/users/{user_id}/avatar.
func (h *Handler) GetActiveAvatar(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing user_id")
		return
	}

	avatar, rc, err := h.service.GetActiveAvatar(r.Context(), userID)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	defer rc.Close()

	setImageHeaders(w, avatar)
	if _, err := io.Copy(w, rc); err != nil {
		h.logger.Error("handler: stream avatar failed", slog.Any("error", err))
	}
}

// GetAvatarMetadata handles GET /api/v1/avatars/{avatar_id}/metadata.
func (h *Handler) GetAvatarMetadata(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "avatar_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid avatar id")
		return
	}
	avatar, err := h.service.GetAvatarMetadata(r.Context(), id)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, toMetadataResponse(avatar))
}

// DeleteAvatar handles DELETE /api/v1/avatars/{avatar_id}.
func (h *Handler) DeleteAvatar(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing X-User-ID header")
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "avatar_id"))
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid avatar id")
		return
	}
	if err := h.service.DeleteAvatar(r.Context(), id, userID); err != nil {
		h.handleServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DeleteActiveUserAvatar handles DELETE /api/v1/users/{user_id}/avatar.
func (h *Handler) DeleteActiveUserAvatar(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing X-User-ID header")
		return
	}
	if err := h.service.DeleteActiveUserAvatar(r.Context(), userID); err != nil {
		h.handleServiceError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// ListUserAvatars handles GET /api/v1/users/{user_id}/avatars.
func (h *Handler) ListUserAvatars(w http.ResponseWriter, r *http.Request) {
	userID := chi.URLParam(r, "user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing user_id")
		return
	}
	avatars, err := h.service.ListUserAvatars(r.Context(), userID)
	if err != nil {
		h.handleServiceError(w, err)
		return
	}
	resp := make([]models.AvatarMetadataResponse, 0, len(avatars))
	for i := range avatars {
		resp = append(resp, toMetadataResponse(&avatars[i]))
	}
	writeJSON(w, http.StatusOK, resp)
}
