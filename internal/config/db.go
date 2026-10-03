// Package config provides database configuration structures and constructors.
package config

import "time"

// DBConf holds database connection settings, including the connection URL,
// migration parameters, and connection pool limits.
type DBConf struct {
	// URL is the database connection string (e.g., postgres://user:pass@host/db).
	URL string
	// ForceMigrations forces migrations to run even if the schema is dirty.
	ForceMigrations bool
	// MaxOpenConns is the maximum number of open connections to the database.
	MaxOpenConns int32
	// MaxIdleConns is the maximum number of idle connections in the pool.
	MaxIdleConns int32
	// ConnMaxLifetime is the maximum amount of time a connection may be reused.
	ConnMaxLifetime time.Duration
	// MigrationsPath is the filesystem path to the directory containing migration files.
	MigrationsPath string
}

// NewDBConf creates a new DBConf with the given database URL and migration flag.
// It applies sensible defaults for connection pool settings:
//   - MaxOpenConns: 10
//   - MaxIdleConns: 5
//   - ConnMaxLifetime: 5 minutes
//   - MigrationsPath: "./migrations"
//
// Parameters:
//   - dbURL: the database connection string (required).
//   - forceMigrations: whether to force migrations in case of dirty schema.
//
// Returns:
//   - *DBConf: a pointer to the initialized configuration.
func NewDBConf(dbURL string, forceMigrations bool) *DBConf {
	return &DBConf{
		URL:             dbURL,
		ForceMigrations: forceMigrations,
		MaxOpenConns:    10,
		MaxIdleConns:    5,
		ConnMaxLifetime: 5 * time.Minute,
		MigrationsPath:  "./migrations",
	}
}
