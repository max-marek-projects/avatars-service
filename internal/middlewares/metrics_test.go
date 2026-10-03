package middlewares_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/max-marek-projects/avatars-service/internal/metrics"
	"github.com/max-marek-projects/avatars-service/internal/middlewares"
)

func TestMetrics_SkipsMetricsPath(t *testing.T) {
	before := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "/metrics", "200"),
	)

	h := middlewares.Metrics(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)

	// Metrics middleware must NOT count its own scrape endpoint.
	after := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "/metrics", "200"),
	)
	assert.Equal(t, before, after)
}

func TestMetrics_RecordsMatchedRoute(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middlewares.Metrics)
	r.Get("/api/v1/avatars/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	before := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/avatars/{id}", "200"),
	)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/avatars/abc", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	after := testutil.ToFloat64(
		metrics.HTTPRequestsTotal.WithLabelValues("GET", "/api/v1/avatars/{id}", "200"),
	)
	assert.Equal(t, before+1, after)
}

func TestMetrics_InFlightGaugeReturnsToZero(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middlewares.Metrics)
	r.Get("/slow", func(w http.ResponseWriter, r *http.Request) {
		// The gauge should be incremented while we're inside the handler.
		v := testutil.ToFloat64(metrics.HTTPInFlight)
		require.GreaterOrEqual(t, v, float64(1))
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/slow", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// After the request, gauge must be back to its previous value.
	assert.Equal(t, float64(0), testutil.ToFloat64(metrics.HTTPInFlight))
}

func TestMetrics_ObservesDurationHistogram(t *testing.T) {
	r := chi.NewRouter()
	r.Use(middlewares.Metrics)
	r.Get("/timed", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	req := httptest.NewRequest(http.MethodGet, "/timed", nil)
	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	count := testutil.CollectAndCount(metrics.HTTPDuration, "http_request_duration_seconds")
	assert.GreaterOrEqual(t, count, 1,
		"after at least one request the histogram family must contain a series")
}
