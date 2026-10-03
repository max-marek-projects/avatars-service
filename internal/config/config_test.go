package config

import (
	"flag"
	"os"
	"testing"
	"time"

	"github.com/max-marek-projects/avatars-service/internal/logger"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// resetEnvAndArgs cleans env and resets flag.CommandLine, restoring them on cleanup.
func resetEnvAndArgs(t *testing.T) {
	t.Helper()
	oldArgs := os.Args
	oldCommandLine := flag.CommandLine
	t.Cleanup(func() {
		os.Args = oldArgs
		flag.CommandLine = oldCommandLine
		os.Clearenv()
	})
	os.Clearenv()
	_ = os.Unsetenv("CONFIG")
	flag.CommandLine = flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
}

func TestLoadConfig_Defaults(t *testing.T) {
	resetEnvAndArgs(t)
	os.Args = []string{"cmd"}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	// HTTP server
	assert.Equal(t, ":8080", cfg.RunAddr)
	assert.Equal(t, 30*time.Second, cfg.ReadTimeout.Duration())
	assert.Equal(t, 30*time.Second, cfg.WriteTimeout.Duration())
	assert.Equal(t, logger.LevelInfo, cfg.LoggerLevel)

	// PostgreSQL
	assert.Empty(t, cfg.DatabaseURI)
	assert.False(t, cfg.ForceMigrations)
	assert.Equal(t, "./migrations", cfg.MigrationsPath)

	// S3 / MinIO
	assert.Equal(t, "minio:9000", cfg.S3Endpoint)
	assert.Empty(t, cfg.S3AccessKey)
	assert.Empty(t, cfg.S3SecretKey)
	assert.Equal(t, "avatars", cfg.S3Bucket)
	assert.False(t, cfg.S3UseSSL)
	assert.Empty(t, cfg.S3Region)

	// RabbitMQ
	assert.Equal(t, "amqp://guest:guest@rabbitmq:5672/", cfg.RabbitURL)
	assert.Equal(t, "avatars.exchange", cfg.RabbitExchange)
	assert.Equal(t, "avatar.uploaded", cfg.RabbitUploadKey)
	assert.Equal(t, "avatar.deleted", cfg.RabbitDeleteKey)

	// Service
	assert.Equal(t, int64(10*1024*1024), cfg.MaxFileSize)
	assert.Equal(t, []string{"image/jpeg", "image/png", "image/webp"}, cfg.AllowedMimeTypes)
	assert.Empty(t, cfg.DefaultAvatarKey)

	// Config file path
	assert.Empty(t, cfg.ConfigFilePath)
}

func TestLoadConfig_Flags(t *testing.T) {
	resetEnvAndArgs(t)
	os.Args = []string{
		"cmd",
		"-a=:9090",
		"-r=5s",
		"-w=10s",
		"-l=ERROR",
		"-d=postgres://flag:pass@localhost:5432/db",
		"-f=true",
		"-migrations=./flag_migrations",
		"-s3-endpoint=s3.example.com:9000",
		"-s3-access-key=flag-access",
		"-s3-secret-key=flag-secret",
		"-s3-bucket=flag-bucket",
		"-s3-use-ssl=true",
		"-s3-region=eu-west-1",
		"-rabbit-url=amqp://flag:flag@rabbit:5672/",
		"-rabbit-exchange=flag.exchange",
		"-rabbit-upload-key=flag.uploaded",
		"-rabbit-delete-key=flag.deleted",
		"-max-file-size=5242880",
		"-allowed-mime-types=image/jpeg,image/png",
		"-default-avatar-key=defaults/placeholder.png",
	}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, ":9090", cfg.RunAddr)
	assert.Equal(t, 5*time.Second, cfg.ReadTimeout.Duration())
	assert.Equal(t, 10*time.Second, cfg.WriteTimeout.Duration())
	assert.Equal(t, logger.LevelError, cfg.LoggerLevel)
	assert.Equal(t, "postgres://flag:pass@localhost:5432/db", cfg.DatabaseURI)
	assert.True(t, cfg.ForceMigrations)
	assert.Equal(t, "./flag_migrations", cfg.MigrationsPath)

	assert.Equal(t, "s3.example.com:9000", cfg.S3Endpoint)
	assert.Equal(t, "flag-access", cfg.S3AccessKey)
	assert.Equal(t, "flag-secret", cfg.S3SecretKey)
	assert.Equal(t, "flag-bucket", cfg.S3Bucket)
	assert.True(t, cfg.S3UseSSL)
	assert.Equal(t, "eu-west-1", cfg.S3Region)

	assert.Equal(t, "amqp://flag:flag@rabbit:5672/", cfg.RabbitURL)
	assert.Equal(t, "flag.exchange", cfg.RabbitExchange)
	assert.Equal(t, "flag.uploaded", cfg.RabbitUploadKey)
	assert.Equal(t, "flag.deleted", cfg.RabbitDeleteKey)

	assert.Equal(t, int64(5*1024*1024), cfg.MaxFileSize)
	assert.Equal(t, []string{"image/jpeg", "image/png"}, cfg.AllowedMimeTypes)
	assert.Equal(t, "defaults/placeholder.png", cfg.DefaultAvatarKey)

	assert.Empty(t, cfg.ConfigFilePath)
}

func TestLoadConfig_Env(t *testing.T) {
	resetEnvAndArgs(t)

	envVars := map[string]string{
		"SERVER_ADDRESS":     ":8080",
		"READ_TIMEOUT":       "15s",
		"WRITE_TIMEOUT":      "20s",
		"LOGGER_LEVEL":       "DEBUG",
		"DATABASE_URI":       "postgres://env:pass@localhost:5432/db",
		"FORCE_MIGRATIONS":   "true",
		"MIGRATIONS":         "./env_migrations",
		"S3_ENDPOINT":        "env-minio:9000",
		"S3_ACCESS_KEY":      "env-access",
		"S3_SECRET_KEY":      "env-secret",
		"S3_BUCKET":          "env-bucket",
		"S3_USE_SSL":         "true",
		"S3_REGION":          "us-east-1",
		"RABBIT_URL":         "amqp://env:env@rabbit:5672/",
		"RABBIT_EXCHANGE":    "env.exchange",
		"RABBIT_UPLOAD_KEY":  "env.uploaded",
		"RABBIT_DELETE_KEY":  "env.deleted",
		"MAX_FILE_SIZE":      "1048576",
		"ALLOWED_MIME_TYPES": "image/png,image/webp",
		"DEFAULT_AVATAR_KEY": "defaults/env.png",
	}
	for k, v := range envVars {
		require.NoError(t, os.Setenv(k, v))
	}

	os.Args = []string{"cmd"}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.RunAddr)
	assert.Equal(t, 15*time.Second, cfg.ReadTimeout.Duration())
	assert.Equal(t, 20*time.Second, cfg.WriteTimeout.Duration())
	assert.Equal(t, logger.LevelDebug, cfg.LoggerLevel)
	assert.Equal(t, "postgres://env:pass@localhost:5432/db", cfg.DatabaseURI)
	assert.True(t, cfg.ForceMigrations)
	assert.Equal(t, "./env_migrations", cfg.MigrationsPath)

	assert.Equal(t, "env-minio:9000", cfg.S3Endpoint)
	assert.Equal(t, "env-access", cfg.S3AccessKey)
	assert.Equal(t, "env-secret", cfg.S3SecretKey)
	assert.Equal(t, "env-bucket", cfg.S3Bucket)
	assert.True(t, cfg.S3UseSSL)
	assert.Equal(t, "us-east-1", cfg.S3Region)

	assert.Equal(t, "amqp://env:env@rabbit:5672/", cfg.RabbitURL)
	assert.Equal(t, "env.exchange", cfg.RabbitExchange)
	assert.Equal(t, "env.uploaded", cfg.RabbitUploadKey)
	assert.Equal(t, "env.deleted", cfg.RabbitDeleteKey)

	assert.Equal(t, int64(1048576), cfg.MaxFileSize)
	assert.Equal(t, []string{"image/png", "image/webp"}, cfg.AllowedMimeTypes)
	assert.Equal(t, "defaults/env.png", cfg.DefaultAvatarKey)

	assert.Empty(t, cfg.ConfigFilePath)
}

func TestLoadConfig_FlagsOverrideEnv(t *testing.T) {
	resetEnvAndArgs(t)

	require.NoError(t, os.Setenv("SERVER_ADDRESS", ":9999"))
	require.NoError(t, os.Setenv("LOGGER_LEVEL", "WARN"))
	require.NoError(t, os.Setenv("S3_BUCKET", "env-bucket"))
	require.NoError(t, os.Setenv("MAX_FILE_SIZE", "1000"))

	os.Args = []string{
		"cmd",
		"-a=:8080",
		"-l=ERROR",
		"-s3-bucket=flag-bucket",
		"-max-file-size=2048",
	}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, ":8080", cfg.RunAddr)
	assert.Equal(t, logger.LevelError, cfg.LoggerLevel)
	assert.Equal(t, "flag-bucket", cfg.S3Bucket)
	assert.Equal(t, int64(2048), cfg.MaxFileSize)
	assert.Empty(t, cfg.ConfigFilePath)
}

func TestLoadConfig_ConfigFile(t *testing.T) {
	resetEnvAndArgs(t)

	tmpFile, err := os.CreateTemp(".", "config*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	content := `{
		"server_address": ":7000",
		"read_timeout": "5s",
		"write_timeout": "10s",
		"logger_level": "DEBUG",
		"database_uri": "postgres://file:pass@localhost:5432/db",
		"force_migrations": true,
		"migrations_path": "./file_migrations",
		"s3_endpoint": "file-minio:9000",
		"s3_access_key": "file-access",
		"s3_secret_key": "file-secret",
		"s3_bucket": "file-bucket",
		"s3_use_ssl": true,
		"s3_region": "ap-south-1",
		"rabbit_url": "amqp://file:file@rabbit:5672/",
		"rabbit_exchange": "file.exchange",
		"rabbit_upload_key": "file.uploaded",
		"rabbit_delete_key": "file.deleted",
		"max_file_size": 2097152,
		"allowed_mime_types": ["image/jpeg","image/webp"],
		"default_avatar_key": "defaults/file.png"
	}`
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	os.Args = []string{"cmd", "-c=" + tmpFile.Name()}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, ":7000", cfg.RunAddr)
	assert.Equal(t, 5*time.Second, cfg.ReadTimeout.Duration())
	assert.Equal(t, 10*time.Second, cfg.WriteTimeout.Duration())
	assert.Equal(t, logger.LevelDebug, cfg.LoggerLevel)
	assert.Equal(t, "postgres://file:pass@localhost:5432/db", cfg.DatabaseURI)
	assert.True(t, cfg.ForceMigrations)
	assert.Equal(t, "./file_migrations", cfg.MigrationsPath)

	assert.Equal(t, "file-minio:9000", cfg.S3Endpoint)
	assert.Equal(t, "file-access", cfg.S3AccessKey)
	assert.Equal(t, "file-secret", cfg.S3SecretKey)
	assert.Equal(t, "file-bucket", cfg.S3Bucket)
	assert.True(t, cfg.S3UseSSL)
	assert.Equal(t, "ap-south-1", cfg.S3Region)

	assert.Equal(t, "amqp://file:file@rabbit:5672/", cfg.RabbitURL)
	assert.Equal(t, "file.exchange", cfg.RabbitExchange)
	assert.Equal(t, "file.uploaded", cfg.RabbitUploadKey)
	assert.Equal(t, "file.deleted", cfg.RabbitDeleteKey)

	assert.Equal(t, int64(2097152), cfg.MaxFileSize)
	assert.Equal(t, []string{"image/jpeg", "image/webp"}, cfg.AllowedMimeTypes)
	assert.Equal(t, "defaults/file.png", cfg.DefaultAvatarKey)

	assert.Equal(t, tmpFile.Name(), cfg.ConfigFilePath)
}

// Test that config file is loaded via CONFIG env variable.
func TestLoadConfig_ConfigFileFromEnv(t *testing.T) {
	resetEnvAndArgs(t)

	tmpFile, err := os.CreateTemp(".", "config*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	content := `{
		"server_address": ":7777",
		"logger_level": "WARN",
		"s3_bucket": "envcfg-bucket"
	}`
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	require.NoError(t, os.Setenv("CONFIG", tmpFile.Name()))

	os.Args = []string{"cmd"}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, ":7777", cfg.RunAddr)
	assert.Equal(t, logger.LevelWarn, cfg.LoggerLevel)
	assert.Equal(t, "envcfg-bucket", cfg.S3Bucket)
	assert.Equal(t, tmpFile.Name(), cfg.ConfigFilePath)
}

// Test that flags override config file.
func TestLoadConfig_FlagsOverrideConfigFile(t *testing.T) {
	resetEnvAndArgs(t)

	tmpFile, err := os.CreateTemp(".", "config*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	content := `{
		"server_address": ":7000",
		"logger_level": "DEBUG",
		"database_uri": "postgres://file:pass@localhost/db",
		"s3_bucket": "file-bucket"
	}`
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	os.Args = []string{
		"cmd",
		"-c=" + tmpFile.Name(),
		"-a=:9000",
		"-l=ERROR",
		"-s3-bucket=flag-bucket",
	}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, ":9000", cfg.RunAddr)
	assert.Equal(t, logger.LevelError, cfg.LoggerLevel)
	assert.Equal(t, "flag-bucket", cfg.S3Bucket)
	// No flag for database_uri -> comes from file.
	assert.Equal(t, "postgres://file:pass@localhost/db", cfg.DatabaseURI)
	assert.Equal(t, tmpFile.Name(), cfg.ConfigFilePath)
}

// Invalid duration in flags.
func TestLoadConfig_InvalidFlagDuration(t *testing.T) {
	resetEnvAndArgs(t)
	os.Args = []string{"cmd", "-r=invalid"}

	_, err := LoadConfig()
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "invalid duration")
}

// Numbers (seconds) as duration in JSON config file.
func TestLoadConfig_ConfigFileWithNumberDuration(t *testing.T) {
	resetEnvAndArgs(t)

	tmpFile, err := os.CreateTemp(".", "config*.json")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	content := `{
		"read_timeout": 10,
		"write_timeout": 20.5
	}`
	_, err = tmpFile.WriteString(content)
	require.NoError(t, err)
	require.NoError(t, tmpFile.Close())

	os.Args = []string{"cmd", "-c=" + tmpFile.Name()}

	cfg, err := LoadConfig()
	require.NoError(t, err)

	assert.Equal(t, 10*time.Second, cfg.ReadTimeout.Duration())
	assert.Equal(t, 20*time.Second, cfg.WriteTimeout.Duration())
}
