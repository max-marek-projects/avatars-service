package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	HTTPRequestsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "http_requests_total",
			Help: "Total HTTP requests",
		},
		[]string{"method", "route", "status"},
	)

	HTTPDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "http_request_duration_seconds",
			Help:    "HTTP request duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"method", "route", "status"},
	)

	HTTPInFlight = prometheus.NewGauge(
		prometheus.GaugeOpts{
			Name: "http_requests_in_flight",
			Help: "Current in-flight requests",
		},
	)

	AvatarUploadsTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "avatar_uploads_total",
			Help: "Avatar uploads by result",
		},
		[]string{"status"},
	)

	AvatarUploadBytes = prometheus.NewHistogram(
		prometheus.HistogramOpts{
			Name:    "avatar_upload_bytes",
			Help:    "Uploaded avatar size in bytes",
			Buckets: []float64{1e4, 1e5, 5e5, 1e6, 5e6, 1e7},
		},
	)

	AvatarDeletesTotal = prometheus.NewCounterVec(
		prometheus.CounterOpts{
			Name: "avatar_deletes_total",
			Help: "Avatar deletions by result",
		},
		[]string{"status"},
	)

	AvatarProcessingDuration = prometheus.NewHistogramVec(
		prometheus.HistogramOpts{
			Name:    "avatar_processing_duration_seconds",
			Help:    "Worker avatar processing duration",
			Buckets: prometheus.DefBuckets,
		},
		[]string{"stage"},
	)
)

func Register() {
	prometheus.MustRegister(
		HTTPRequestsTotal, HTTPDuration, HTTPInFlight,
		AvatarUploadsTotal, AvatarUploadBytes, AvatarDeletesTotal,
		AvatarProcessingDuration,
	)
}
