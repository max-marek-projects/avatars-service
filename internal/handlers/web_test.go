package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/max-marek-projects/avatars-service/internal/models"
)

func newWebHandler(t *testing.T) (*Handler, *MockService) {
	t.Helper()
	svc := NewMockService(t)
	h := NewHandler(svc, nil, 10<<20)
	return h, svc
}

func TestWebUploadForm_RendersForm(t *testing.T) {
	h, _ := newWebHandler(t)

	req := httptest.NewRequest(http.MethodGet, "/web/upload", nil)
	rr := httptest.NewRecorder()
	h.WebUploadForm(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	assert.Contains(t, rr.Header().Get("Content-Type"), "text/html")
	body := rr.Body.String()
	assert.Contains(t, body, "Upload avatar")
	assert.Contains(t, body, `action="/web/upload"`)
	assert.Contains(t, body, `name="user_id"`)
	assert.Contains(t, body, `name="file"`)
}

func TestWebUpload_RedirectsToGallery(t *testing.T) {
	h, svc := newWebHandler(t)

	id := uuid.New()
	svc.EXPECT().
		UploadAvatar(mock.Anything, "42", mock.Anything, "a.png", mock.Anything, mock.Anything).
		Return(&models.Avatar{ID: id, UserID: "42"}, nil)

	var buf strings.Builder
	buf.WriteString("--B\r\nContent-Disposition: form-data; name=\"user_id\"\r\n\r\n42\r\n")
	buf.WriteString("--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\n")
	buf.WriteString("Content-Type: image/png\r\n\r\n")
	buf.WriteString("PNG")
	buf.WriteString("\r\n--B--\r\n")

	req := httptest.NewRequest(http.MethodPost, "/web/upload", strings.NewReader(buf.String()))
	req.Header.Set("Content-Type", `multipart/form-data; boundary=B`)
	rr := httptest.NewRecorder()
	h.WebUpload(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/web/gallery/42", rr.Header().Get("Location"))
}

func TestWebUpload_MissingUserID(t *testing.T) {
	h, _ := newWebHandler(t)

	body := "--B\r\nContent-Disposition: form-data; name=\"file\"; filename=\"a.png\"\r\n\r\nPNG\r\n--B--\r\n"
	req := httptest.NewRequest(http.MethodPost, "/web/upload", strings.NewReader(body))
	req.Header.Set("Content-Type", `multipart/form-data; boundary=B`)
	rr := httptest.NewRecorder()
	h.WebUpload(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "user_id is required")
}

func TestWebGallery_RendersAvatars(t *testing.T) {
	h, svc := newWebHandler(t)

	id := uuid.New()
	svc.EXPECT().ListUserAvatars(mock.Anything, "42").Return([]models.Avatar{
		{ID: id, UserID: "42", FileName: "a.png"},
	}, nil)

	r := chi.NewRouter()
	r.Get("/web/gallery/{user_id}", h.WebGallery)

	req := httptest.NewRequest(http.MethodGet, "/web/gallery/42", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	require.Equal(t, http.StatusOK, rr.Code)
	body := rr.Body.String()
	assert.Contains(t, body, "Gallery — 42")
	assert.Contains(t, body, id.String())
	assert.Contains(t, body, "/web/avatars/"+id.String()+"/delete")
}

func TestWebGallery_ServiceErrorReturnsError(t *testing.T) {
	h, svc := newWebHandler(t)

	svc.EXPECT().ListUserAvatars(mock.Anything, "42").
		Return(nil, assert.AnError)

	r := chi.NewRouter()
	r.Get("/web/gallery/{user_id}", h.WebGallery)

	req := httptest.NewRequest(http.MethodGet, "/web/gallery/42", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusInternalServerError, rr.Code)
}

func TestWebDeleteAvatar_RedirectsToGallery(t *testing.T) {
	h, svc := newWebHandler(t)

	id := uuid.New()
	svc.EXPECT().DeleteAvatar(mock.Anything, id, "42").Return(nil)

	r := chi.NewRouter()
	r.Post("/web/avatars/{avatar_id}/delete", h.WebDeleteAvatar)

	form := url.Values{"user_id": {"42"}}
	req := httptest.NewRequest(http.MethodPost,
		"/web/avatars/"+id.String()+"/delete",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusSeeOther, rr.Code)
	assert.Equal(t, "/web/gallery/42", rr.Header().Get("Location"))
}

func TestWebDeleteAvatar_InvalidUUID(t *testing.T) {
	h, _ := newWebHandler(t)

	r := chi.NewRouter()
	r.Post("/web/avatars/{avatar_id}/delete", h.WebDeleteAvatar)

	form := url.Values{"user_id": {"42"}}
	req := httptest.NewRequest(http.MethodPost, "/web/avatars/not-a-uuid/delete",
		strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "invalid avatar id")
}

func TestWebDeleteAvatar_MissingUserID(t *testing.T) {
	h, _ := newWebHandler(t)

	r := chi.NewRouter()
	r.Post("/web/avatars/{avatar_id}/delete", h.WebDeleteAvatar)

	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost,
		"/web/avatars/"+id.String()+"/delete",
		strings.NewReader(""))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusBadRequest, rr.Code)
	assert.Contains(t, rr.Body.String(), "user_id is required")
}
