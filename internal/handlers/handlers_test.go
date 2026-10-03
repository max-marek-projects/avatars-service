package handlers

import (
	"bytes"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"net/textproto"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
)

const testUserID = "user-1"

func newAPITestRouter(h *Handler) http.Handler {
	r := chi.NewRouter()
	r.Get("/health", h.Health)
	r.Route("/api/v1", func(r chi.Router) {
		r.Route("/avatars", func(r chi.Router) {
			r.Post("/", h.UploadAvatar)
			r.Get("/{avatar_id}", h.GetAvatarByID)
			r.Delete("/{avatar_id}", h.DeleteAvatar)
			r.Get("/{avatar_id}/metadata", h.GetAvatarMetadata)
		})
		r.Route("/users/{user_id}", func(r chi.Router) {
			r.Get("/avatar", h.GetActiveAvatar)
			r.Delete("/avatar", h.DeleteActiveUserAvatar)
			r.Get("/avatars", h.ListUserAvatars)
		})
	})
	return r
}

// testRequest performs an HTTP request with an optional X-User-ID header.
// userID == "" means "no header".
func testRequest(
	t *testing.T,
	ts *httptest.Server,
	method, path, body, userID string,
) (*http.Response, string) {
	t.Helper()

	var reader io.Reader
	if body != "" {
		reader = bytes.NewBufferString(body)
	}

	req, err := http.NewRequest(method, ts.URL+path, reader)
	require.NoError(t, err)
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	client := ts.Client()
	client.CheckRedirect = func(req *http.Request, via []*http.Request) error {
		return http.ErrUseLastResponse
	}
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(respBody)
}

// testMultipartRequest sends a multipart/form-data POST with a single "file"
// part, setting Content-Type on the part itself (CreateFormFile does not).
func testMultipartRequest(
	t *testing.T,
	ts *httptest.Server,
	path, fileName, contentType string,
	content []byte,
	userID string,
) (*http.Response, string) {
	t.Helper()

	var buf bytes.Buffer
	mw := multipart.NewWriter(&buf)

	h := make(textproto.MIMEHeader)
	h.Set("Content-Disposition",
		fmt.Sprintf(`form-data; name="file"; filename="%s"`, fileName))
	h.Set("Content-Type", contentType)
	fw, err := mw.CreatePart(h)
	require.NoError(t, err)
	_, err = fw.Write(content)
	require.NoError(t, err)
	require.NoError(t, mw.Close())

	req, err := http.NewRequest(http.MethodPost, ts.URL+path, &buf)
	require.NoError(t, err)
	req.Header.Set("Content-Type", mw.FormDataContentType())
	if userID != "" {
		req.Header.Set("X-User-ID", userID)
	}

	resp, err := ts.Client().Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	respBody, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return resp, string(respBody)
}
