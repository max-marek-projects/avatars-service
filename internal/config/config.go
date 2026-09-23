// Package config handles application configuration from flags, env, and .env.
// It supports loading from environment variables, .env file, JSON config file,
// and command-line flags with the following precedence:
//
//  1. Command-line flags (highest)
//  2. JSON config file (if provided via -config or CONFIG env)
//  3. Environment variables
//  4. .env file
//  5. Default values (lowest)
//
// All fields are documented with their env and JSON tags.
package config

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/caarlos0/env/v11"
	"github.com/joho/godotenv"
	"github.com/max-marek-projects/avatars-service/internal/logger"
	"github.com/max-marek-projects/avatars-service/internal/models"
)

// Config holds all configuration parameters for the application.
type Config struct {
	// ---------- HTTP server ----------

	// RunAddr is the address and port for the HTTP server (e.g., ":8080").
	RunAddr string `env:"SERVER_ADDRESS" json:"server_address"`
	// ReadTimeout is the maximum duration for reading the entire request.
	ReadTimeout models.Duration `env:"READ_TIMEOUT" json:"read_timeout"`
	// WriteTimeout is the maximum duration before timing out writes of the response.
	WriteTimeout models.Duration `env:"WRITE_TIMEOUT" json:"write_timeout"`

	// ---------- Logging ----------

	// LoggerLevel is the log level (DEBUG, INFO, WARN, ERROR).
	LoggerLevel logger.Level `env:"LOGGER_LEVEL" json:"logger_level"`

	// ---------- PostgreSQL ----------

	// DatabaseURI is the connection string for the PostgreSQL database.
	DatabaseURI string `env:"DATABASE_URI" json:"database_uri"`
	// ForceMigrations forces database migrations even if the schema is dirty.
	ForceMigrations bool `env:"FORCE_MIGRATIONS" json:"force_migrations"`
	// MigrationsPath is the directory containing migration files.
	MigrationsPath string `env:"MIGRATIONS" json:"migrations_path"`

	// ---------- S3 / MinIO ----------

	// S3Endpoint is the host:port of the S3-compatible backend (e.g., "minio:9000").
	S3Endpoint string `env:"S3_ENDPOINT" json:"s3_endpoint"`
	// S3AccessKey is the access key ID.
	S3AccessKey string `env:"S3_ACCESS_KEY" json:"s3_access_key"`
	// S3SecretKey is the secret access key.
	S3SecretKey string `env:"S3_SECRET_KEY" json:"s3_secret_key"`
	// S3Bucket is the bucket name for avatar objects.
	S3Bucket string `env:"S3_BUCKET" json:"s3_bucket"`
	// S3UseSSL enables HTTPS for the S3 endpoint.
	S3UseSSL bool `env:"S3_USE_SSL" json:"s3_use_ssl"`
	// S3Region is the S3 region (may be empty for MinIO).
	S3Region string `env:"S3_REGION" json:"s3_region"`

	// ---------- RabbitMQ ----------

	// RabbitURL is the AMQP connection string (e.g., "amqp://guest:guest@rabbitmq:5672/").
	RabbitURL string `env:"RABBIT_URL" json:"rabbit_url"`
	// RabbitExchange is the topic exchange used for avatar events.
	RabbitExchange string `env:"RABBIT_EXCHANGE" json:"rabbit_exchange"`
	// RabbitUploadKey is the routing key for avatar upload events.
	RabbitUploadKey string `env:"RABBIT_UPLOAD_KEY" json:"rabbit_upload_key"`
	// RabbitDeleteKey is the routing key for avatar delete events.
	RabbitDeleteKey string `env:"RABBIT_DELETE_KEY" json:"rabbit_delete_key"`

	// ---------- Service ----------

	// MaxFileSize is the maximum allowed upload size in bytes.
	MaxFileSize int64 `env:"MAX_FILE_SIZE" json:"max_file_size"`
	// AllowedMimeTypes is the whitelist of accepted image content types.
	// In env it is a comma-separated list, e.g. "image/jpeg,image/png,image/webp".
	AllowedMimeTypes []string `env:"ALLOWED_MIME_TYPES" envSeparator:"," json:"allowed_mime_types"`
	// DefaultAvatarKey is the S3 key of the placeholder image returned
	// for users without an uploaded avatar. Empty disables the placeholder.
	DefaultAvatarKey string `env:"DEFAULT_AVATAR_KEY" json:"default_avatar_key"`

	// ConfigFilePath is the path to a JSON configuration file (ignored in JSON).
	ConfigFilePath string `env:"CONFIG" json:"-"`

	// StaticDir is the filesystem path to the directory with SPA assets
	// (index.html and friends). Empty disables static serving.
	StaticDir string `env:"STATIC_DIR" json:"static_dir"`
}

// LoadConfig loads and returns the application configuration.
// It applies defaults, reads .env, environment variables, config file (if specified),
// and finally command-line flags. Flags take precedence over all other sources.
//
// Returns:
//   - *Config: populated configuration.
//   - error: non-nil if .env parsing fails, environment parsing fails,
//     config file reading or unmarshaling fails.
func LoadConfig() (config *Config, err error) {
	// Default configuration.
	config = &Config{
		RunAddr:          ":8080",
		LoggerLevel:      logger.LevelInfo,
		MigrationsPath:   "./migrations",
		ReadTimeout:      models.Duration(30 * time.Second),
		WriteTimeout:     models.Duration(30 * time.Second),
		S3Endpoint:       "minio:9000",
		S3Bucket:         "avatars",
		S3UseSSL:         false,
		RabbitURL:        "amqp://guest:guest@rabbitmq:5672/",
		RabbitExchange:   "avatars.exchange",
		RabbitUploadKey:  "avatar.uploaded",
		RabbitDeleteKey:  "avatar.deleted",
		MaxFileSize:      10 << 20, // 10 MiB
		AllowedMimeTypes: []string{"image/jpeg", "image/png", "image/webp"},
		StaticDir:        "./web/static",
	}

	// Parse .env file.
	err = godotenv.Load()
	if err != nil {
		if os.IsNotExist(err) {
			fmt.Printf("No .env file found, using environment variables and flags")
		} else {
			return nil, fmt.Errorf("failed to load .env file: %w", err)
		}
	}
	if err := env.Parse(config); err != nil {
		return nil, fmt.Errorf("failed to parse env: %w", err)
	}

	// Parse config file path.
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "-c" || args[i] == "-config":
			if i+1 < len(args) {
				config.ConfigFilePath = args[i+1]
			}
		case strings.HasPrefix(args[i], "-c="):
			config.ConfigFilePath = strings.TrimPrefix(args[i], "-c=")
		case strings.HasPrefix(args[i], "-config="):
			config.ConfigFilePath = strings.TrimPrefix(args[i], "-config=")
		}
	}

	// Parse config file.
	if config.ConfigFilePath != "" {
		// #nosec G703 -- configuration file path is intentionally provided by the user.
		data, err := os.ReadFile(config.ConfigFilePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read config file: %w", err)
		}
		if err := json.Unmarshal(data, config); err != nil {
			return nil, fmt.Errorf("failed to parse config file: %w", err)
		}
	}

	// Read flags directly to config.
	fs := flag.NewFlagSet("", flag.ContinueOnError)
	fs.StringVar(&config.RunAddr, "a", config.RunAddr, "address and port to run http server")
	fs.Var(&config.LoggerLevel, "l", "logger level")
	fs.StringVar(&config.DatabaseURI, "d", config.DatabaseURI, "database connection url")
	fs.BoolVar(&config.ForceMigrations, "f", config.ForceMigrations, "force database migrations in case of last dirty versions")
	fs.StringVar(&config.MigrationsPath, "migrations", config.MigrationsPath, "path to database migrations")
	fs.StringVar(&config.ConfigFilePath, "c", config.ConfigFilePath, "config file path")
	fs.StringVar(&config.ConfigFilePath, "config", config.ConfigFilePath, "config file path")
	fs.Var(&config.ReadTimeout, "r", "server read timeout")
	fs.Var(&config.WriteTimeout, "w", "server write timeout")

	// S3 / MinIO.
	fs.StringVar(&config.S3Endpoint, "s3-endpoint", config.S3Endpoint, "S3 endpoint host:port")
	fs.StringVar(&config.S3AccessKey, "s3-access-key", config.S3AccessKey, "S3 access key ID")
	fs.StringVar(&config.S3SecretKey, "s3-secret-key", config.S3SecretKey, "S3 secret access key")
	fs.StringVar(&config.S3Bucket, "s3-bucket", config.S3Bucket, "S3 bucket name")
	fs.BoolVar(&config.S3UseSSL, "s3-use-ssl", config.S3UseSSL, "use HTTPS for S3")
	fs.StringVar(&config.S3Region, "s3-region", config.S3Region, "S3 region")

	// RabbitMQ.
	fs.StringVar(&config.RabbitURL, "rabbit-url", config.RabbitURL, "RabbitMQ AMQP URL")
	fs.StringVar(&config.RabbitExchange, "rabbit-exchange", config.RabbitExchange, "RabbitMQ topic exchange")
	fs.StringVar(&config.RabbitUploadKey, "rabbit-upload-key", config.RabbitUploadKey, "RabbitMQ routing key for upload events")
	fs.StringVar(&config.RabbitDeleteKey, "rabbit-delete-key", config.RabbitDeleteKey, "RabbitMQ routing key for delete events")

	// Service.
	fs.Int64Var(&config.MaxFileSize, "max-file-size", config.MaxFileSize, "maximum upload size in bytes")
	fs.StringVar(&config.DefaultAvatarKey, "default-avatar-key", config.DefaultAvatarKey, "S3 key of the default avatar placeholder")
	mimeTypes := strings.Join(config.AllowedMimeTypes, ",")
	fs.StringVar(&mimeTypes, "allowed-mime-types", mimeTypes, "comma-separated list of allowed MIME types")
	fs.StringVar(&config.StaticDir, "static-dir", config.StaticDir, "path to static web assets")

	if err := fs.Parse(os.Args[1:]); err != nil {
		return nil, fmt.Errorf("failed to parse flags: %w", err)
	}
	config.AllowedMimeTypes = splitAndTrim(mimeTypes)

	return config, nil
}

// splitAndTrim splits a comma-separated string and trims whitespace around each item,
// dropping empty entries. Returns nil for an empty input.
func splitAndTrim(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
