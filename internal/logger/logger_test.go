package logger

import (
	"flag"
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLevelSet(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantLevel Level
		wantErr   bool
	}{
		{"valid DEBUG", "DEBUG", LevelDebug, false},
		{"valid INFO", "INFO", LevelInfo, false},
		{"valid WARN", "WARN", LevelWarn, false},
		{"valid ERROR", "ERROR", LevelError, false},
		{"invalid lower", "debug", "", true},
		{"invalid value", "FATAL", "", true},
		{"empty", "", "", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var l Level
			err := l.Set(tt.input)
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, tt.wantLevel, l)
			}
		})
	}
}

func TestLevelMarshalUnmarshal(t *testing.T) {
	t.Run("marshal", func(t *testing.T) {
		data, err := LevelDebug.MarshalText()
		require.NoError(t, err)
		assert.Equal(t, "DEBUG", string(data))
	})

	t.Run("unmarshal", func(t *testing.T) {
		var l Level
		err := l.UnmarshalText([]byte("WARN"))
		require.NoError(t, err)
		assert.Equal(t, LevelWarn, l)
	})

	t.Run("unmarshal invalid", func(t *testing.T) {
		var l Level
		err := l.UnmarshalText([]byte("invalid"))
		assert.Error(t, err)
	})
}

func TestInitialize(t *testing.T) {
	tests := []struct {
		name    string
		level   Level
		wantErr bool
	}{
		{"valid DEBUG", LevelDebug, false},
		{"valid INFO", LevelInfo, false},
		{"valid WARN", LevelWarn, false},
		{"valid ERROR", LevelError, false},
		{"invalid level", Level("FATAL"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logger, err := New(tt.level)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Contains(t, err.Error(), "invalid log level")
			} else {
				require.NoError(t, err)
				assert.NotNil(t, logger)
				assert.Equal(t, logger, slog.Default())
				// Check that the handler uses JSON and correct level.
				assert.IsType(t, &slog.JSONHandler{}, logger.Handler())
			}
		})
	}
}

func TestFlagIntegration(t *testing.T) {
	// Simulate flag parsing with a custom flagset.
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	var l Level
	fs.Var(&l, "level", "log level")
	err := fs.Parse([]string{"-level", "DEBUG"})
	require.NoError(t, err)
	assert.Equal(t, LevelDebug, l)
}
