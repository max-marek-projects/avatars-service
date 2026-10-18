package tracing

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestInit_EmptyEndpoint_ReturnsNoop(t *testing.T) {
	shutdown, err := Init(context.Background(), Config{})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestInit_Defaults(t *testing.T) {
	shutdown, err := Init(context.Background(), Config{
		Endpoint:       "127.0.0.1:1",
		ServiceVersion: "test",
		Insecure:       true,
	})
	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NoError(t, shutdown(context.Background()))
}

func TestInit_SampleRatioClamping(t *testing.T) {
	for _, r := range []float64{-1, 0, 2} {
		_, err := Init(context.Background(), Config{
			Endpoint:    "127.0.0.1:1",
			SampleRatio: r,
			Insecure:    true,
		})
		require.NoError(t, err, "ratio=%v", r)
	}
}
