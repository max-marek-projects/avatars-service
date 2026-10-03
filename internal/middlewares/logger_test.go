package middlewares

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type capturedRecord struct {
	Level   slog.Level
	Message string
	Attrs   map[string]slog.Value
}

type captureHandler struct {
	records []capturedRecord
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *captureHandler) Handle(_ context.Context, r slog.Record) error {
	rec := capturedRecord{
		Level:   r.Level,
		Message: r.Message,
		Attrs:   make(map[string]slog.Value),
	}
	r.Attrs(func(a slog.Attr) bool {
		rec.Attrs[a.Key] = a.Value
		return true
	})
	h.records = append(h.records, rec)
	return nil
}

func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

func (h *captureHandler) last() capturedRecord {
	return h.records[len(h.records)-1]
}

type errWriter struct {
	http.ResponseWriter
	err error
}

func (w *errWriter) Write([]byte) (int, error) { return 0, w.err }

type partialWriter struct {
	http.ResponseWriter
	n   int
	err error
}

func (w *partialWriter) Write([]byte) (int, error) { return w.n, w.err }

func TestRequestsLogger_LogsAllFields(t *testing.T) {
	h := &captureHandler{}
	logger := slog.New(h)

	const body = "hello world"
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(body))
	})

	mw := RequestsLogger(logger)(next)

	req := httptest.NewRequest(http.MethodPost, "/api/v1/avatars?x=1", nil)
	rec := httptest.NewRecorder()
	mw.ServeHTTP(rec, req)

	require.Len(t, h.records, 1)
	got := h.last()

	assert.Equal(t, slog.LevelInfo, got.Level)
	assert.Equal(t, "Processed request", got.Message)

	assert.Equal(t, "/api/v1/avatars?x=1", got.Attrs["uri"].String())
	assert.Equal(t, http.MethodPost, got.Attrs["method"].String())
	assert.Equal(t, int64(http.StatusCreated), got.Attrs["status"].Int64())
	assert.Equal(t, int64(len(body)), got.Attrs["size"].Int64())

	_, ok := got.Attrs["duration"]
	assert.True(t, ok, "duration attribute must be present")
}

func TestRequestsLogger_PassesThroughToNextHandler(t *testing.T) {
	h := &captureHandler{}
	logger := slog.New(h)

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called = true
		w.WriteHeader(http.StatusTeapot)
	})

	rec := httptest.NewRecorder()
	RequestsLogger(logger)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/x", nil))

	assert.True(t, called, "next handler must be invoked")
	assert.Equal(t, http.StatusTeapot, rec.Code)
	assert.Equal(t, int64(http.StatusTeapot), h.last().Attrs["status"].Int64())
}

func TestRequestsLogger_AccumulatesBodySize(t *testing.T) {
	h := &captureHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("aaa"))
		_, _ = w.Write([]byte("bbbb"))
		_, _ = w.Write([]byte("c"))
	})

	RequestsLogger(logger)(next).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	require.Len(t, h.records, 1)
	assert.Equal(t, int64(8), h.last().Attrs["size"].Int64())
}

func TestRequestsLogger_ZeroSizeWhenNoBody(t *testing.T) {
	h := &captureHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	})

	RequestsLogger(logger)(next).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodDelete, "/x", nil))

	assert.Equal(t, int64(0), h.last().Attrs["size"].Int64())
	assert.Equal(t, int64(http.StatusNoContent), h.last().Attrs["status"].Int64())
}

func TestRequestsLogger_StatusStaysZeroWhenWriteHeaderNotCalled(t *testing.T) {
	h := &captureHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})

	rec := httptest.NewRecorder()
	RequestsLogger(logger)(next).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	assert.Equal(t, http.StatusOK, rec.Code, "net/http returns 200 implicitly")
	assert.Equal(t, int64(0), h.last().Attrs["status"].Int64(),
		"middleware does not see the implicit 200")
}

func TestRequestsLogger_DurationIsMeasured(t *testing.T) {
	h := &captureHandler{}
	logger := slog.New(h)

	const delay = 20 * time.Millisecond
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(delay)
	})

	RequestsLogger(logger)(next).
		ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	require.Len(t, h.records, 1)
	got := h.last().Attrs["duration"].Duration()
	assert.GreaterOrEqual(t, got, delay, "duration must cover the handler sleep")
}

func TestRequestsLogger_LogsEachRequestSeparately(t *testing.T) {
	h := &captureHandler{}
	logger := slog.New(h)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mw := RequestsLogger(logger)(next)

	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/a", nil))
	mw.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/b", nil))

	require.Len(t, h.records, 2)
	assert.Equal(t, "/a", h.records[0].Attrs["uri"].String())
	assert.Equal(t, "/b", h.records[1].Attrs["uri"].String())
}

func TestRequestsLogger_NilLoggerPanics(t *testing.T) {
	require.Panics(t, func() {
		next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
		RequestsLogger(nil)(next).
			ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))
	})
}

func TestLoggingResponseWriter_WriteForwardsToInnerWriter(t *testing.T) {
	rec := httptest.NewRecorder()
	rd := &responseData{}
	lw := loggingResponseWriter{ResponseWriter: rec, responseData: rd}

	n, err := lw.Write([]byte("abc"))
	require.NoError(t, err)
	assert.Equal(t, 3, n)
	assert.Equal(t, "abc", rec.Body.String())
	assert.Equal(t, 3, rd.size)
}

func TestLoggingResponseWriter_WriteAccumulatesSize(t *testing.T) {
	rec := httptest.NewRecorder()
	rd := &responseData{}
	lw := loggingResponseWriter{ResponseWriter: rec, responseData: rd}

	_, _ = lw.Write([]byte("111"))
	_, _ = lw.Write([]byte("22222"))
	_, _ = lw.Write([]byte("3"))

	assert.Equal(t, 9, rd.size)
	assert.Equal(t, "111222223", rec.Body.String())
}

func TestLoggingResponseWriter_WriteWrapsError(t *testing.T) {
	inner := errors.New("disk full")
	rd := &responseData{}
	lw := loggingResponseWriter{
		ResponseWriter: &errWriter{ResponseWriter: httptest.NewRecorder(), err: inner},
		responseData:   rd,
	}

	n, err := lw.Write([]byte("hello"))
	assert.Equal(t, 0, n)
	require.Error(t, err)
	assert.ErrorIs(t, err, inner, "original error must be wrapped, not replaced")
	assert.Contains(t, err.Error(), "failed to write response")
}

func TestLoggingResponseWriter_WriteAccumulatesOnPartialWrite(t *testing.T) {
	rd := &responseData{}
	lw := loggingResponseWriter{
		ResponseWriter: &partialWriter{n: 3, err: errors.New("boom")},
		responseData:   rd,
	}

	n, err := lw.Write([]byte("hello"))
	assert.Equal(t, 3, n)
	assert.Error(t, err)
	assert.Equal(t, 3, rd.size)
}

func TestLoggingResponseWriter_WriteHeaderCapturesStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	rd := &responseData{}
	lw := loggingResponseWriter{ResponseWriter: rec, responseData: rd}

	lw.WriteHeader(http.StatusNotFound)

	assert.Equal(t, http.StatusNotFound, rec.Code)
	assert.Equal(t, http.StatusNotFound, rd.status)
}

func TestLoggingResponseWriter_WriteHeaderSeveralCodes(t *testing.T) {
	cases := []int{
		http.StatusOK,
		http.StatusCreated,
		http.StatusNoContent,
		http.StatusBadRequest,
		http.StatusForbidden,
		http.StatusNotFound,
		http.StatusInternalServerError,
	}

	for _, code := range cases {
		t.Run(http.StatusText(code), func(t *testing.T) {
			rec := httptest.NewRecorder()
			rd := &responseData{}
			lw := loggingResponseWriter{ResponseWriter: rec, responseData: rd}

			lw.WriteHeader(code)

			assert.Equal(t, code, rec.Code)
			assert.Equal(t, code, rd.status)
		})
	}
}

func TestLoggingResponseWriter_SecondWriteHeaderOverwritesCapturedStatus(t *testing.T) {
	rec := httptest.NewRecorder()
	rd := &responseData{}
	lw := loggingResponseWriter{ResponseWriter: rec, responseData: rd}

	lw.WriteHeader(http.StatusOK)
	lw.WriteHeader(http.StatusInternalServerError)

	assert.Equal(t, http.StatusOK, rec.Code,
		"net/http keeps the first code")
	assert.Equal(t, http.StatusInternalServerError, rd.status,
		"wrapper overwrites captured status on every call")
}

func TestLoggingResponseWriter_PreservesHeaders(t *testing.T) {
	rec := httptest.NewRecorder()
	rd := &responseData{}
	lw := loggingResponseWriter{ResponseWriter: rec, responseData: rd}

	lw.Header().Set("Content-Type", "image/png")
	lw.Header().Set("Cache-Control", "public, max-age=86400")
	lw.WriteHeader(http.StatusOK)

	assert.Equal(t, "image/png", rec.Header().Get("Content-Type"))
	assert.Equal(t, "public, max-age=86400", rec.Header().Get("Cache-Control"))
}
