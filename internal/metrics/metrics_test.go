package metrics

import "testing"

func TestRegister_NoPanic(t *testing.T) {
	Register()
}

func TestCountersIncrementable(t *testing.T) {
	HTTPRequestsTotal.WithLabelValues("GET", "/x", "200").Inc()
	AvatarUploadsTotal.WithLabelValues("success").Inc()
	AvatarUploadBytes.Observe(1024)
	AvatarProcessingDuration.WithLabelValues("upload").Observe(0.1)
}
