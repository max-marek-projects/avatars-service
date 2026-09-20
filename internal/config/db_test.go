package config

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestNewDBConf(t *testing.T) {
	t.Run("returns configuration with correct defaults", func(t *testing.T) {
		dbURL := "postgres://user:pass@localhost:5432/mydb"
		force := true
		cfg := NewDBConf(dbURL, force)

		assert.Equal(t, dbURL, cfg.URL)
		assert.Equal(t, force, cfg.ForceMigrations)
		assert.Equal(t, int32(10), cfg.MaxOpenConns)
		assert.Equal(t, int32(5), cfg.MaxIdleConns)
		assert.Equal(t, 5*time.Minute, cfg.ConnMaxLifetime)
		assert.Equal(t, "./migrations", cfg.MigrationsPath)
	})

	t.Run("works with empty URL and false force", func(t *testing.T) {
		cfg := NewDBConf("", false)
		assert.Equal(t, "", cfg.URL)
		assert.False(t, cfg.ForceMigrations)
		// Defaults unchanged
		assert.Equal(t, int32(10), cfg.MaxOpenConns)
		assert.Equal(t, int32(5), cfg.MaxIdleConns)
		assert.Equal(t, 5*time.Minute, cfg.ConnMaxLifetime)
		assert.Equal(t, "./migrations", cfg.MigrationsPath)
	})

	t.Run("all fields are settable after creation", func(t *testing.T) {
		cfg := NewDBConf("", false)
		cfg.URL = "new-url"
		cfg.MaxOpenConns = 20
		cfg.MaxIdleConns = 10
		cfg.ConnMaxLifetime = 10 * time.Minute
		cfg.MigrationsPath = "/custom/path"

		assert.Equal(t, "new-url", cfg.URL)
		assert.Equal(t, int32(20), cfg.MaxOpenConns)
		assert.Equal(t, int32(10), cfg.MaxIdleConns)
		assert.Equal(t, 10*time.Minute, cfg.ConnMaxLifetime)
		assert.Equal(t, "/custom/path", cfg.MigrationsPath)
	})
}
