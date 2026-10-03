package middlewares

import (
	"fmt"
	"log/slog"
	"net/http"
	"time"
)

// RequestsLogger is a middleware that logs each HTTP request's URI, method, status, duration, and response size.
// Parameters:
//   - h: the next http.Handler in the chain.
//
// Returns an http.Handler that performs logging before delegating.
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

			duration := time.Since(start)

			logger.Info(
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
	// responseData holds HTTP response status code and body size for logging.
	responseData struct {
		status int
		size   int
	}

	// loggingResponseWriter wraps http.ResponseWriter to capture status code and body size.
	loggingResponseWriter struct {
		http.ResponseWriter
		responseData *responseData
	}
)

// Write captures the number of bytes written and delegates to the underlying ResponseWriter.
func (r *loggingResponseWriter) Write(b []byte) (int, error) {
	size, err := r.ResponseWriter.Write(b)
	r.responseData.size += size
	if err != nil {
		return size, fmt.Errorf("failed to write response: %w", err)
	}
	return size, err
}

// WriteHeader captures the status code and delegates to the underlying ResponseWriter.
func (r *loggingResponseWriter) WriteHeader(statusCode int) {
	r.ResponseWriter.WriteHeader(statusCode)
	r.responseData.status = statusCode
}
