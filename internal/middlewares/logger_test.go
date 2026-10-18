package middlewares

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"log/slog"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newCaptureLogger returns a logger that writes JSON records into buf.
// It is used to inspect fields emitted by RequestsLogger.
func newCaptureLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	h := slog.NewJSONHandler(buf, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(h), buf
}

// lastEntry parses the last JSON line written to buf.
// slog writes one record per line; we keep the most recent one.
func lastEntry(t *testing.T, buf *bytes.Buffer) map[string]any {
	t.Helper()
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	require.NotEmpty(t, lines, "no log entries captured")

	var entry map[string]any
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
	return entry
}

// ---------- main scenarios ----------

// Handler writes a body without calling WriteHeader explicitly.
// net/http then sends an implicit 200. The middleware must log that.
func TestRequestsLogger_InfersImplicit200(t *testing.T) {
	log, buf := newCaptureLogger(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("hello"))
	})

	h := RequestsLogger(log)(next)

	req := httptest.NewRequest(http.MethodGet, "/test", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	entry := lastEntry(t, buf)
	assert.Equal(t, float64(http.StatusOK), entry["status"], "implicit 200 must be logged")
	assert.Equal(t, float64(len("hello")), entry["size"])
	assert.Equal(t, "GET", entry["method"])
	assert.Equal(t, "/test", entry["uri"])
}

// Handler sets 201 explicitly. The log must reflect that value.
func TestRequestsLogger_RespectsExplicitStatus(t *testing.T) {
	log, buf := newCaptureLogger(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte("created"))
	})

	h := RequestsLogger(log)(next)

	req := httptest.NewRequest(http.MethodPost, "/thing", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	entry := lastEntry(t, buf)
	assert.Equal(t, float64(http.StatusCreated), entry["status"])
	assert.Equal(t, float64(len("created")), entry["size"])
}

// Handler writes nothing. net/http still sends an implicit 200 with an
// empty body. Size stays 0, but status must be 200 in the log.
func TestRequestsLogger_NoBody(t *testing.T) {
	log, buf := newCaptureLogger(t)

	next := http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})

	h := RequestsLogger(log)(next)

	req := httptest.NewRequest(http.MethodGet, "/empty", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	entry := lastEntry(t, buf)
	assert.Equal(t, float64(http.StatusOK), entry["status"])
	assert.Equal(t, float64(0), entry["size"])
}

// Multiple Write calls must be summed into size.
func TestRequestsLogger_AccumulatesSize(t *testing.T) {
	log, buf := newCaptureLogger(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte("abc"))
		_, _ = w.Write([]byte("defgh"))
	})

	h := RequestsLogger(log)(next)

	req := httptest.NewRequest(http.MethodGet, "/chunks", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	entry := lastEntry(t, buf)
	assert.Equal(t, float64(len("abcdefgh")), entry["size"])
}

// The middleware must not modify the outgoing response.
func TestRequestsLogger_PassesThroughResponse(t *testing.T) {
	log, _ := newCaptureLogger(t)

	next := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusTeapot)
		_, _ = w.Write([]byte("teapot"))
	})

	h := RequestsLogger(log)(next)

	req := httptest.NewRequest(http.MethodGet, "/teapot", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusTeapot, rr.Code)
	assert.Equal(t, "teapot", rr.Body.String())
}

// ---------- loggingResponseWriter ----------

func TestLoggingResponseWriter_WriteHeaderSetsStatus(t *testing.T) {
	rr := httptest.NewRecorder()
	data := &responseData{}
	lw := &loggingResponseWriter{ResponseWriter: rr, responseData: data}

	lw.WriteHeader(http.StatusAccepted)

	assert.Equal(t, http.StatusAccepted, data.status)
	assert.Equal(t, http.StatusAccepted, rr.Code)
}

func TestLoggingResponseWriter_WriteAccumulatesSize(t *testing.T) {
	rr := httptest.NewRecorder()
	data := &responseData{}
	lw := &loggingResponseWriter{ResponseWriter: rr, responseData: data}

	n, err := lw.Write([]byte("hello"))
	require.NoError(t, err)
	assert.Equal(t, 5, n)
	assert.Equal(t, 5, data.size)

	_, err = lw.Write([]byte("world"))
	require.NoError(t, err)
	assert.Equal(t, 10, data.size)
}

// ---------- auxiliary ----------

// The middleware must not replace the request context.
func TestRequestsLogger_ContextPassthrough(t *testing.T) {
	log, _ := newCaptureLogger(t)

	type ctxKey struct{}
	next := http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		require.Equal(t, "value", r.Context().Value(ctxKey{}))
	})

	h := RequestsLogger(log)(next)

	req := httptest.NewRequest(http.MethodGet, "/ctx", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "value"))
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)
}
