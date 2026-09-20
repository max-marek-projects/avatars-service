package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/max-marek-projects/avatars-service/internal/services"
)

// ---------- POST /api/v1/avatars ----------

func TestUploadAvatar(t *testing.T) {
	uploadedAvatar := &models.Avatar{
		ID:               uuid.New(),
		UserID:           testUserID,
		FileName:         "avatar.png",
		MimeType:         "image/png",
		SizeBytes:        4,
		S3Key:            "avatars/user-1/x.png",
		ThumbnailS3Keys:  map[string]string{},
		UploadStatus:     models.UploadStatusUploaded,
		ProcessingStatus: models.ProcessingStatusPending,
	}

	mockService := NewMockService(t)
	mockService.EXPECT().
		UploadAvatar(mock.Anything, testUserID, mock.Anything, "avatar.png", "image/png", int64(4)).
		Return(uploadedAvatar, nil)
	mockService.EXPECT().
		UploadAvatar(mock.Anything, testUserID, mock.Anything, "big.png", "image/png", int64(5)).
		Return(nil, services.ErrFileTooLarge)
	mockService.EXPECT().
		UploadAvatar(mock.Anything, testUserID, mock.Anything, "bad.txt", "text/plain", int64(4)).
		Return(nil, services.ErrUnsupportedFormat)
	mockService.EXPECT().
		UploadAvatar(mock.Anything, testUserID, mock.Anything, "boom.png", "image/png", int64(4)).
		Return(nil, errors.New("db down"))

	ts := httptest.NewServer(newAPITestRouter(NewHandler(mockService, nil)))
	defer ts.Close()

	type want struct {
		code  int
		check func(t *testing.T, body string)
	}
	tests := []struct {
		name        string
		fileName    string
		contentType string
		content     string
		userID      string
		want        want
	}{
		{
			name:        "created",
			fileName:    "avatar.png",
			contentType: "image/png",
			content:     "data",
			userID:      testUserID,
			want: want{
				code: http.StatusCreated,
				check: func(t *testing.T, body string) {
					var resp models.UploadAvatarResponse
					require.NoError(t, json.Unmarshal([]byte(body), &resp))
					assert.Equal(t, uploadedAvatar.ID.String(), resp.ID)
					assert.Equal(t, string(models.ProcessingStatusPending), resp.Status)
				},
			},
		},
		{
			name:        "missing X-User-ID",
			fileName:    "avatar.png",
			contentType: "image/png",
			content:     "data",
			userID:      "",
			want:        want{code: http.StatusBadRequest},
		},
		{
			name:        "file too large",
			fileName:    "big.png",
			contentType: "image/png",
			content:     "datas",
			userID:      testUserID,
			want:        want{code: http.StatusRequestEntityTooLarge},
		},
		{
			name:        "unsupported format",
			fileName:    "bad.txt",
			contentType: "text/plain",
			content:     "data",
			userID:      testUserID,
			want:        want{code: http.StatusBadRequest},
		},
		{
			name:        "service error",
			fileName:    "boom.png",
			contentType: "image/png",
			content:     "data",
			userID:      testUserID,
			want:        want{code: http.StatusInternalServerError},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, body := testMultipartRequest(
				t, ts, "/api/v1/avatars",
				test.fileName, test.contentType, []byte(test.content),
				test.userID,
			)
			assert.Equal(t, test.want.code, resp.StatusCode)
			if test.want.check != nil {
				test.want.check(t, body)
			}
		})
	}
	mockService.AssertExpectations(t)
}

// ---------- GET /api/v1/avatars/{avatar_id} ----------

func TestGetAvatarByID(t *testing.T) {
	id := uuid.New()
	successAvatar := &models.Avatar{
		ID:       id,
		MimeType: "image/png",
		FileName: "avatar.png",
	}

	mockService := NewMockService(t)
	mockService.EXPECT().
		GetAvatarByID(mock.Anything, id).
		Return(successAvatar, io.NopCloser(strings.NewReader("PNGDATA")), nil)
	mockService.EXPECT().
		GetAvatarByID(mock.Anything, mock.Anything).
		Return(nil, nil, services.ErrAvatarNotFound)

	ts := httptest.NewServer(newAPITestRouter(NewHandler(mockService, nil)))
	defer ts.Close()

	type want struct {
		code     int
		body     string
		mimeType string
	}
	tests := []struct {
		name string
		path string
		want want
	}{
		{
			name: "success",
			path: "/api/v1/avatars/" + id.String(),
			want: want{
				code:     http.StatusOK,
				body:     "PNGDATA",
				mimeType: "image/png",
			},
		},
		{
			name: "invalid uuid",
			path: "/api/v1/avatars/not-a-uuid",
			want: want{code: http.StatusBadRequest},
		},
		{
			name: "not found",
			path: "/api/v1/avatars/" + uuid.New().String(),
			want: want{code: http.StatusNotFound},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, body := testRequest(t, ts, http.MethodGet, test.path, "", "")
			assert.Equal(t, test.want.code, resp.StatusCode)
			if test.want.body != "" {
				assert.Equal(t, test.want.body, body)
				assert.Equal(t, test.want.mimeType, resp.Header.Get("Content-Type"))
			}
		})
	}
	mockService.AssertExpectations(t)
}

// ---------- GET /api/v1/avatars/{avatar_id}/metadata ----------

func TestGetAvatarMetadata(t *testing.T) {
	id := uuid.New()
	mockService := NewMockService(t)
	mockService.EXPECT().
		GetAvatarMetadata(mock.Anything, id).
		Return(&models.Avatar{
			ID:              id,
			UserID:          testUserID,
			FileName:        "avatar.png",
			MimeType:        "image/png",
			SizeBytes:       42,
			ThumbnailS3Keys: map[string]string{"100x100": "k1"},
		}, nil)
	mockService.EXPECT().
		GetAvatarMetadata(mock.Anything, mock.Anything).
		Return(nil, services.ErrAvatarNotFound)

	ts := httptest.NewServer(newAPITestRouter(NewHandler(mockService, nil)))
	defer ts.Close()

	type want struct {
		code  int
		check func(t *testing.T, body string)
	}
	tests := []struct {
		name string
		path string
		want want
	}{
		{
			name: "success",
			path: "/api/v1/avatars/" + id.String() + "/metadata",
			want: want{
				code: http.StatusOK,
				check: func(t *testing.T, body string) {
					var resp models.AvatarMetadataResponse
					require.NoError(t, json.Unmarshal([]byte(body), &resp))
					assert.Equal(t, id.String(), resp.ID)
					require.Len(t, resp.Thumbnails, 1)
					assert.Equal(t, "100x100", resp.Thumbnails[0].Size)
				},
			},
		},
		{
			name: "invalid uuid",
			path: "/api/v1/avatars/nope/metadata",
			want: want{code: http.StatusBadRequest},
		},
		{
			name: "not found",
			path: "/api/v1/avatars/" + uuid.New().String() + "/metadata",
			want: want{code: http.StatusNotFound},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, body := testRequest(t, ts, http.MethodGet, test.path, "", "")
			assert.Equal(t, test.want.code, resp.StatusCode)
			if test.want.check != nil {
				test.want.check(t, body)
			}
		})
	}
	mockService.AssertExpectations(t)
}

// ---------- DELETE /api/v1/avatars/{avatar_id} ----------

func TestDeleteAvatar(t *testing.T) {
	okID := uuid.New()
	forbiddenID := uuid.New()
	notFoundID := uuid.New()

	mockService := NewMockService(t)
	mockService.EXPECT().
		DeleteAvatar(mock.Anything, okID, testUserID).
		Return(nil)
	mockService.EXPECT().
		DeleteAvatar(mock.Anything, forbiddenID, testUserID).
		Return(services.ErrForbidden)
	mockService.EXPECT().
		DeleteAvatar(mock.Anything, notFoundID, testUserID).
		Return(services.ErrAvatarNotFound)

	ts := httptest.NewServer(newAPITestRouter(NewHandler(mockService, nil)))
	defer ts.Close()

	tests := []struct {
		name   string
		path   string
		userID string
		want   int
	}{
		{"204 no content", "/api/v1/avatars/" + okID.String(), testUserID, http.StatusNoContent},
		{"missing header", "/api/v1/avatars/" + okID.String(), "", http.StatusBadRequest},
		{"forbidden", "/api/v1/avatars/" + forbiddenID.String(), testUserID, http.StatusForbidden},
		{"not found", "/api/v1/avatars/" + notFoundID.String(), testUserID, http.StatusNotFound},
		{"invalid uuid", "/api/v1/avatars/nope", testUserID, http.StatusBadRequest},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, _ := testRequest(t, ts, http.MethodDelete, test.path, "", test.userID)
			assert.Equal(t, test.want, resp.StatusCode)
		})
	}
	mockService.AssertExpectations(t)
}

// ---------- GET /api/v1/users/{user_id}/avatar ----------

func TestGetActiveAvatar(t *testing.T) {
	mockService := NewMockService(t)
	mockService.EXPECT().
		GetActiveAvatar(mock.Anything, testUserID).
		Return(&models.Avatar{
			ID:       uuid.New(),
			UserID:   testUserID,
			MimeType: "image/png",
			FileName: "avatar.png",
		}, io.NopCloser(strings.NewReader("ACTIVE")), nil)
	mockService.EXPECT().
		GetActiveAvatar(mock.Anything, mock.Anything).
		Return(nil, nil, services.ErrAvatarNotFound)

	ts := httptest.NewServer(newAPITestRouter(NewHandler(mockService, nil)))
	defer ts.Close()

	type want struct {
		code int
		body string
	}
	tests := []struct {
		name string
		path string
		want want
	}{
		{
			name: "success",
			path: "/api/v1/users/" + testUserID + "/avatar",
			want: want{code: http.StatusOK, body: "ACTIVE"},
		},
		{
			name: "not found",
			path: "/api/v1/users/unknown/avatar",
			want: want{code: http.StatusNotFound},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, body := testRequest(t, ts, http.MethodGet, test.path, "", "")
			assert.Equal(t, test.want.code, resp.StatusCode)
			if test.want.body != "" {
				assert.Equal(t, test.want.body, body)
			}
		})
	}
	mockService.AssertExpectations(t)
}

// ---------- DELETE /api/v1/users/{user_id}/avatar ----------

func TestDeleteActiveUserAvatar(t *testing.T) {
	mockService := NewMockService(t)
	mockService.EXPECT().
		DeleteActiveUserAvatar(mock.Anything, "active-user").
		Return(nil)
	mockService.EXPECT().
		DeleteActiveUserAvatar(mock.Anything, "missing-user").
		Return(services.ErrAvatarNotFound)

	ts := httptest.NewServer(newAPITestRouter(NewHandler(mockService, nil)))
	defer ts.Close()

	tests := []struct {
		name   string
		path   string
		userID string
		want   int
	}{
		{"204 no content", "/api/v1/users/active-user/avatar", "active-user", http.StatusNoContent},
		{"missing header", "/api/v1/users/active-user/avatar", "", http.StatusBadRequest},
		{"not found", "/api/v1/users/missing-user/avatar", "missing-user", http.StatusNotFound},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, _ := testRequest(t, ts, http.MethodDelete, test.path, "", test.userID)
			assert.Equal(t, test.want, resp.StatusCode)
		})
	}
	mockService.AssertExpectations(t)
}

// ---------- GET /api/v1/users/{user_id}/avatars ----------

func TestListUserAvatars(t *testing.T) {
	mockService := NewMockService(t)
	mockService.EXPECT().
		ListUserAvatars(mock.Anything, testUserID).
		Return([]models.Avatar{
			{ID: uuid.New(), UserID: testUserID, FileName: "a.png", MimeType: "image/png"},
			{ID: uuid.New(), UserID: testUserID, FileName: "b.png", MimeType: "image/png"},
		}, nil)
	mockService.EXPECT().
		ListUserAvatars(mock.Anything, "empty").
		Return([]models.Avatar{}, nil)
	mockService.EXPECT().
		ListUserAvatars(mock.Anything, "boom").
		Return(nil, errors.New("db down"))

	ts := httptest.NewServer(newAPITestRouter(NewHandler(mockService, nil)))
	defer ts.Close()

	type want struct {
		code  int
		count int
	}
	tests := []struct {
		name string
		path string
		want want
	}{
		{
			name: "success",
			path: "/api/v1/users/" + testUserID + "/avatars",
			want: want{code: http.StatusOK, count: 2},
		},
		{
			name: "empty list",
			path: "/api/v1/users/empty/avatars",
			want: want{code: http.StatusOK, count: 0},
		},
		{
			name: "service error",
			path: "/api/v1/users/boom/avatars",
			want: want{code: http.StatusInternalServerError},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			resp, body := testRequest(t, ts, http.MethodGet, test.path, "", "")
			assert.Equal(t, test.want.code, resp.StatusCode)
			if test.want.code == http.StatusOK {
				var resp []models.AvatarMetadataResponse
				require.NoError(t, json.Unmarshal([]byte(body), &resp))
				assert.Len(t, resp, test.want.count)
			}
		})
	}
	mockService.AssertExpectations(t)
}

// ---------- GET /health ----------

func TestHealth(t *testing.T) {
	okService := NewMockService(t)
	okService.EXPECT().
		Health(mock.Anything).
		Return(models.HealthStatus{
			Status: "ok",
			Details: map[string]models.ComponentHealth{
				"database": {Status: "ok"},
				"s3":       {Status: "ok"},
				"broker":   {Status: "ok"},
			},
		})

	degradedService := NewMockService(t)
	degradedService.EXPECT().
		Health(mock.Anything).
		Return(models.HealthStatus{
			Status: "degraded",
			Details: map[string]models.ComponentHealth{
				"database": {Status: "unavailable", Error: "down"},
			},
		})

	t.Run("200 ok", func(t *testing.T) {
		ts := httptest.NewServer(newAPITestRouter(NewHandler(okService, nil)))
		defer ts.Close()

		resp, _ := testRequest(t, ts, http.MethodGet, "/health", "", "")
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		okService.AssertExpectations(t)
	})

	t.Run("503 degraded", func(t *testing.T) {
		ts := httptest.NewServer(newAPITestRouter(NewHandler(degradedService, nil)))
		defer ts.Close()

		resp, _ := testRequest(t, ts, http.MethodGet, "/health", "", "")
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
		degradedService.AssertExpectations(t)
	})
}
