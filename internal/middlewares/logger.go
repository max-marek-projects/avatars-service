package middlewares

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// RequestsLogger returns a middleware that logs each request's URI, method,
// status code, duration, and response size.
//
// net/http sends an implicit 200 OK when a handler returns without writing
// anything. Since the wrapper cannot observe that call directly, the status
// is normalized to http.StatusOK after the handler returns if it is still 0.
func RequestsLogger(logger *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()

			responseData := &responseData{
				status: 0,
				size:   0,
			}
			lw := loggingResponseWriter{
				ResponseWriter: w,
				responseData:   responseData,
			}

			next.ServeHTTP(&lw, r)

			// net/http sends an implicit 200 OK when a handler returns
			// without writing anything. The wrapper cannot observe that
			// call, so normalize a still-zero status to 200 before logging.
			if responseData.status == 0 {
				responseData.status = http.StatusOK
			}

			duration := time.Since(start)

			logger.InfoContext(
				r.Context(),
				"Processed request",
				slog.String("uri", r.RequestURI),
				slog.String("method", r.Method),
				slog.Int("status", responseData.status),
				slog.Duration("duration", duration),
				slog.Int("size", responseData.size),
			)
		})
	}
}

type (
	// responseData captures the HTTP status code and body size for logging.
	responseData struct {
		status int
		size   int
	}

	// loggingResponseWriter wraps http.ResponseWriter to capture the status
	// code and body size of the response.
	loggingResponseWriter struct {
		http.ResponseWriter
		responseData *responseData
	}
)

// Write captures the number of bytes written and delegates to the
// underlying ResponseWriter.
func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	if err != nil {
		return size, fmt.Errorf("failed to write response: %w", err)
	}
	return size, nil
}

// WriteHeader captures the first status code and delegates to the underlying
// ResponseWriter. Subsequent calls are forwarded but do not overwrite the
// recorded status, matching net/http semantics where only the first call
// takes effect.
func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	if r.responseData.status == 0 {
		r.responseData.status = statusCode
	}
	r.ResponseWriter.WriteHeader(statusCode)
}
