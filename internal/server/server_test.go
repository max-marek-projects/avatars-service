package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/max-marek-projects/avatars-service/internal/handlers"
	"github.com/max-marek-projects/avatars-service/internal/models"
	"github.com/max-marek-projects/avatars-service/internal/services"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

const testUserID = "user-1"

// newTestServer starts an httptest.Server backed by the real chi router.
// staticDir is empty: tests never mount the SPA.
func newTestServer(t *testing.T, svc Service) *httptest.Server {
	t.Helper()
	h := handlers.NewHandler(svc, nil)
	ts := httptest.NewServer(NewRouter(h, "", nil))
	t.Cleanup(ts.Close)
	return ts
}

// newMultipartBody builds a multipart body with a single "file" part,
// setting Content-Type on the part itself (CreateFormFile does not).
func newMultipartBody(
	t *testing.T,
	fieldName, fileName, contentType string,
	content []byte,
) (*bytes.Buffer, string) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="%s"; filename="%s"`, fieldName, fileName))
	h.Set("Content-Type", contentType)

	fw, err := mw.CreatePart(h)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	return &buf, mw.FormDataContentType()
}

// ---------- lifecycle ----------

func TestNewServer(t *testing.T) {
	svc := NewMockService(t)
	h := handlers.NewHandler(svc, nil)

	t.Run("ok", func(t *testing.T) {
		srv, err := NewServer("127.0.0.1:0", h, time.Second, time.Second, "", nil)
		require.NoError(t, err)
		assert.NotNil(t, srv.Router())
	})

	t.Run("nil handler", func(t *testing.T) {
		_, err := NewServer("127.0.0.1:0", nil, 0, 0, "", nil)
		assert.Error(t, err)
	})
}

func TestServer_ListenAndServe_Shutdown(t *testing.T) {
	svc := NewMockService(t)
	svc.On("Close", mock.Anything).Return(nil)
	h := handlers.NewHandler(svc, nil)

	srv, err := NewServer("127.0.0.1:0", h, time.Second, time.Second, "", nil)
	require.NoError(t, err)

	errCh := make(chan error, 1)
	go func() { errCh <- srv.ListenAndServe() }()

	time.Sleep(50 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	require.NoError(t, srv.Shutdown(ctx))

	select {
	case err := <-errCh:
		assert.NoError(t, err)
	case <-time.After(3 * time.Second):
		t.Fatal("server did not stop in time")
	}
	svc.AssertExpectations(t)
}

// ---------- HTTP handlers via router ----------

func TestRouter_UploadAvatar(t *testing.T) {
	t.Run("201", func(t *testing.T) {
		svc := NewMockService(t)
		av := &models.Avatar{
			ID:               uuid.New(),
			UserID:           testUserID,
			FileName:         "a.png",
			MimeType:         "image/png",
			SizeBytes:        4,
			ProcessingStatus: models.ProcessingStatusPending,
		}
		svc.On("UploadAvatar", mock.Anything, testUserID, mock.Anything, "a.png", "image/png", int64(4)).
			Return(av, nil)

		ts := newTestServer(t, svc)
		body, ct := newMultipartBody(t, "file", "a.png", "image/png", []byte("data"))
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/avatars", body)
		req.Header.Set("Content-Type", ct)
		req.Header.Set("X-User-ID", testUserID)
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()

		assert.Equal(t, http.StatusCreated, resp.StatusCode)

		var p models.UploadAvatarResponse
		require.NoError(t, json.NewDecoder(resp.Body).Decode(&p))
		assert.Equal(t, av.ID.String(), p.ID)
		svc.AssertExpectations(t)
	})

	t.Run("400 missing header", func(t *testing.T) {
		svc := NewMockService(t)
		ts := newTestServer(t, svc)
		body, ct := newMultipartBody(t, "file", "a.png", "image/png", []byte("data"))
		req, _ := http.NewRequest(http.MethodPost, ts.URL+"/api/v1/avatars", body)
		req.Header.Set("Content-Type", ct)
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusBadRequest, resp.StatusCode)
	})
}

func TestRouter_GetAvatarByID(t *testing.T) {
	t.Run("200 streams bytes", func(t *testing.T) {
		svc := NewMockService(t)
		id := uuid.New()
		av := &models.Avatar{ID: id, MimeType: "image/png", FileName: "a.png"}
		svc.On("GetAvatarByID", mock.Anything, id).
			Return(av, io.NopCloser(strings.NewReader("PNGDATA")), nil)

		ts := newTestServer(t, svc)
		resp, err := ts.Client().Get(ts.URL + "/api/v1/avatars/" + id.String())
		require.NoError(t, err)
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)
		assert.Equal(t, http.StatusOK, resp.StatusCode)
		assert.Equal(t, "image/png", resp.Header.Get("Content-Type"))
		assert.Equal(t, "PNGDATA", string(body))
	})

	t.Run("404", func(t *testing.T) {
		svc := NewMockService(t)
		id := uuid.New()
		svc.On("GetAvatarByID", mock.Anything, id).
			Return(nil, nil, services.ErrAvatarNotFound)

		ts := newTestServer(t, svc)
		resp, err := ts.Client().Get(ts.URL + "/api/v1/avatars/" + id.String())
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNotFound, resp.StatusCode)
	})
}

func TestRouter_DeleteAvatar(t *testing.T) {
	t.Run("204", func(t *testing.T) {
		svc := NewMockService(t)
		id := uuid.New()
		svc.On("DeleteAvatar", mock.Anything, id, testUserID).Return(nil)

		ts := newTestServer(t, svc)
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/avatars/"+id.String(), nil)
		req.Header.Set("X-User-ID", testUserID)
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusNoContent, resp.StatusCode)
	})

	t.Run("403", func(t *testing.T) {
		svc := NewMockService(t)
		id := uuid.New()
		svc.On("DeleteAvatar", mock.Anything, id, testUserID).Return(services.ErrForbidden)

		ts := newTestServer(t, svc)
		req, _ := http.NewRequest(http.MethodDelete, ts.URL+"/api/v1/avatars/"+id.String(), nil)
		req.Header.Set("X-User-ID", testUserID)
		resp, err := ts.Client().Do(req)
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusForbidden, resp.StatusCode)
	})
}

func TestRouter_Health(t *testing.T) {
	t.Run("200 ok", func(t *testing.T) {
		svc := NewMockService(t)
		svc.On("Health", mock.Anything).Return(models.HealthStatus{
			Status: "ok",
			Details: map[string]models.ComponentHealth{
				"database": {Status: "ok"},
				"s3":       {Status: "ok"},
				"broker":   {Status: "ok"},
			},
		})
		ts := newTestServer(t, svc)
		resp, err := ts.Client().Get(ts.URL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusOK, resp.StatusCode)
	})

	t.Run("503 degraded", func(t *testing.T) {
		svc := NewMockService(t)
		svc.On("Health", mock.Anything).Return(models.HealthStatus{
			Status: "degraded",
			Details: map[string]models.ComponentHealth{
				"database": {Status: "unavailable", Error: "down"},
			},
		})
		ts := newTestServer(t, svc)
		resp, err := ts.Client().Get(ts.URL + "/health")
		require.NoError(t, err)
		defer resp.Body.Close()
		assert.Equal(t, http.StatusServiceUnavailable, resp.StatusCode)
	})
}
