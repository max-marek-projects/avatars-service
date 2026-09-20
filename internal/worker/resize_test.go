package worker

import (
	"bytes"
	"image"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestResize(t *testing.T) {
	src := makePNG(t, 800, 400)

	t.Run("square 100", func(t *testing.T) {
		out, err := Resize(bytes.NewReader(src), 100)
		require.NoError(t, err)
		cfg, _, err := image.DecodeConfig(bytes.NewReader(out))
		require.NoError(t, err)
		assert.Equal(t, 100, cfg.Width)
		assert.Equal(t, 100, cfg.Height)
	})

	t.Run("invalid size", func(t *testing.T) {
		_, err := Resize(bytes.NewReader(src), 0)
		assert.Error(t, err)
	})

	t.Run("not an image", func(t *testing.T) {
		_, err := Resize(bytes.NewReader([]byte("hello")), 100)
		assert.Error(t, err)
	})
}
