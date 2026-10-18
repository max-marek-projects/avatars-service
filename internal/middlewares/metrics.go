package middlewares

import (
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/max-marek-projects/avatars-service/internal/metrics"
)

func Metrics(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/metrics" {
			next.ServeHTTP(w, r)
			return
		}

		metrics.HTTPInFlight.Inc()
		defer metrics.HTTPInFlight.Dec()

		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)

		next.ServeHTTP(ww, r)

		route := chi.RouteContext(r.Context()).RoutePattern()
		if route == "" {
			route = "unknown"
		}
		status := strconv.Itoa(ww.Status())
		metrics.HTTPRequestsTotal.WithLabelValues(r.Method, route, status).Inc()
		metrics.HTTPDuration.WithLabelValues(r.Method, route, status).
			Observe(time.Since(start).Seconds())
	})
}
