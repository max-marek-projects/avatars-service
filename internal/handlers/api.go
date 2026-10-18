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

// Health godoc
//
//	@Summary		Liveness / readiness probe
//	@Description	Checks PostgreSQL, S3/MinIO, RabbitMQ. Returns 200 if all are ok, 503 otherwise.
//	@Tags			Health
//	@Produce		json
//	@Success		200	{object}	models.HealthStatus
//	@Failure		503	{object}	models.HealthStatus
//	@Router			/health [get]
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	status := h.service.Health(r.Context())
	code := http.StatusOK
	if status.Status != "ok" {
		code = http.StatusServiceUnavailable
	}
	writeJSON(w, code, status)
}

// UploadAvatar godoc
//
//	@Summary		Upload a new avatar
//	@Description	Accepts multipart/form-data with a single "file" field. Allowed MIME: image/jpeg, image/png, image/webp. Size limit: MAX_FILE_SIZE.
//	@Tags			Avatars
//	@Accept			multipart/form-data
//	@Produce		json
//	@Security		XUserID
//	@Param			file	formData	file	true	"Avatar image file"
//	@Success		201		{object}	models.UploadAvatarResponse
//	@Failure		400		{object}	models.ErrorResponse
//	@Failure		413		{object}	models.ErrorResponse
//	@Failure		503		{object}	models.ErrorResponse
//	@Router			/api/v1/avatars [post]
func (h *Handler) UploadAvatar(w http.ResponseWriter, r *http.Request) {
	userID := r.Header.Get("X-User-ID")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "missing X-User-ID header")
		return
	}

	// Enforce the hard cap before touching the body.
	r.Body = http.MaxBytesReader(w, r.Body, h.maxUploadSize)
	if err := r.ParseMultipartForm(h.maxUploadSize); err != nil {
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

// GetAvatarByID godoc
//
//	@Summary		Download avatar by ID
//	@Description	Returns the original avatar image as binary.
//	@Tags			Avatars
//	@Produce		image/jpeg,image/png,image/webp
//	@Param			avatar_id	path	string	true	"Avatar UUID"
//	@Success		200			{file}	binary
//	@Failure		400			{object}	models.ErrorResponse
//	@Failure		404			{object}	models.ErrorResponse
//	@Router			/api/v1/avatars/{avatar_id} [get]
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
		h.logger.DebugContext(r.Context(), "handler: stream avatar failed", slog.Any("error", err))
	}
}

// GetActiveAvatar godoc
//
//	@Summary		Active avatar of the user
//	@Description	Returns the freshest avatar or a placeholder. Binary image.
//	@Tags			Users
//	@Produce		image/jpeg,image/png,image/webp
//	@Param			user_id	path	string	true	"User ID"
//	@Success		200		{file}	binary
//	@Failure		404		{object}	models.ErrorResponse
//	@Router			/api/v1/users/{user_id}/avatar [get]
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
		h.logger.DebugContext(r.Context(), "handler: stream avatar failed", slog.Any("error", err))
	}
}

// GetAvatarMetadata godoc
//
//	@Summary		Avatar metadata
//	@Description	Returns JSON metadata without touching S3.
//	@Tags			Avatars
//	@Produce		json
//	@Param			avatar_id	path	string	true	"Avatar UUID"
//	@Success		200			{object}	models.AvatarMetadataResponse
//	@Failure		400			{object}	models.ErrorResponse
//	@Failure		404			{object}	models.ErrorResponse
//	@Router			/api/v1/avatars/{avatar_id}/metadata [get]
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

// DeleteAvatar godoc
//
//	@Summary		Delete avatar (owner only)
//	@Description	Soft-delete. Verifies X-User-ID matches owner.
//	@Tags			Avatars
//	@Security		XUserID
//	@Param			avatar_id	path	string	true	"Avatar UUID"
//	@Success		204
//	@Failure		400	{object}	models.ErrorResponse
//	@Failure		403	{object}	models.ErrorResponse
//	@Failure		404	{object}	models.ErrorResponse
//	@Router			/api/v1/avatars/{avatar_id} [delete]
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

// DeleteActiveUserAvatar godoc
//
//	@Summary		Delete active avatar of the user
//	@Tags			Users
//	@Security		XUserID
//	@Param			user_id	path	string	true	"User ID"
//	@Success		204
//	@Failure		400	{object}	models.ErrorResponse
//	@Failure		404	{object}	models.ErrorResponse
//	@Router			/api/v1/users/{user_id}/avatar [delete]
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

// ListUserAvatars godoc
//
//	@Summary		List user avatars
//	@Description	Returns all non-deleted avatars of the user.
//	@Tags			Users
//	@Produce		json
//	@Param			user_id	path	string	true	"User ID"
//	@Success		200		{array}	models.AvatarMetadataResponse
//	@Failure		500		{object}	models.ErrorResponse
//	@Router			/api/v1/users/{user_id}/avatars [get]
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
