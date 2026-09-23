package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/golang-migrate/migrate/v4"
	"github.com/golang-migrate/migrate/v4/database/postgres"
	_ "github.com/golang-migrate/migrate/v4/source/file"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/max-marek-projects/avatars-service/internal/config"
	"github.com/max-marek-projects/avatars-service/internal/models"
)

// connect establishes a connection to the database and returns a pgxpool.Pool.
func connect(ctx context.Context, cfg *config.DBConf) (*pgxpool.Pool, error) {
	poolConfig, err := pgxpool.ParseConfig(cfg.URL)
	if err != nil {
		return nil, fmt.Errorf("failed to parse database URL: %w", err)
	}
	poolConfig.MaxConns = cfg.MaxOpenConns
	poolConfig.MinConns = cfg.MaxIdleConns
	poolConfig.MaxConnLifetime = cfg.ConnMaxLifetime

	pool, err := pgxpool.NewWithConfig(ctx, poolConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create connection pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("connection to the DB is not ready: %w", err)
	}
	return pool, nil
}

// DBPool is a minimal abstraction over pgxpool.Pool for mocking.
type DBPool interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
	BeginTx(context.Context, pgx.TxOptions) (pgx.Tx, error)
	Ping(context.Context) error
	Close()
}

// dbStorage implements AvatarRepository using PostgreSQL.
type dbStorage struct {
	storage DBPool
	config  *config.DBConf
	logger  *slog.Logger
}

// NewDBStorage creates a new database storage and runs migrations.
func NewDBStorage(ctx context.Context, dbURL string, forceMigrations bool, logger *slog.Logger) (*dbStorage, error) {
	cfg := config.NewDBConf(dbURL, forceMigrations)

	pool, err := connect(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create DB storage: %w", err)
	}

	dbs := &dbStorage{
		storage: pool,
		config:  cfg,
		logger:  logger,
	}
	if err := dbs.runMigrations(pool); err != nil {
		pool.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}
	return dbs, nil
}

// runMigrations applies database migrations from the configured path.
func (dbs *dbStorage) runMigrations(pool *pgxpool.Pool) error {
	db := stdlib.OpenDBFromPool(pool)
	defer db.Close()

	dbs.logger.Info("Running migrations", slog.String("path", dbs.config.MigrationsPath))

	driver, err := postgres.WithInstance(db, &postgres.Config{})
	if err != nil {
		return fmt.Errorf("failed to create postgres migration driver: %w", err)
	}
	m, err := migrate.NewWithDatabaseInstance(
		"file://"+dbs.config.MigrationsPath,
		"postgres",
		driver,
	)
	if err != nil {
		return fmt.Errorf("failed to create migrations: %w", err)
	}
	defer m.Close()

	if err := m.Up(); err != nil && !errors.Is(err, migrate.ErrNoChange) {
		if errDirty, ok := errors.AsType[migrate.ErrDirty](err); ok && dbs.config.ForceMigrations {
			dbs.logger.Warn("Database is dirty, forcing to previous version",
				slog.Int("dirty_version", errDirty.Version))
			if err := m.Force(max(errDirty.Version-1, 1)); err != nil {
				return fmt.Errorf("failed to force version: %w", err)
			}
			return dbs.runMigrations(pool)
		}
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	return nil
}

// Close closes the database connection pool.
func (dbs *dbStorage) Close(_ context.Context) error {
	dbs.storage.Close()
	return nil
}

// Ping checks whether the database is reachable (used by /health).
func (dbs *dbStorage) Ping(ctx context.Context) error {
	return dbs.storage.Ping(ctx)
}

// ========== AVATARS ==========

const avatarColumns = `id, user_id, file_name, mime_type, size_bytes,
	s3_key, thumbnail_s3_keys, upload_status, processing_status,
	created_at, updated_at, deleted_at`

// CreateAvatar inserts new avatar metadata.
func (dbs *dbStorage) CreateAvatar(ctx context.Context, a *models.Avatar) error {
	if a == nil {
		return fmt.Errorf("nil avatar: %w", ErrInvalidArgument)
	}
	if a.ID == uuid.Nil {
		return fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}
	if a.UserID == "" {
		return fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}
	if a.S3Key == "" {
		return fmt.Errorf("empty s3 key: %w", ErrInvalidArgument)
	}

	var thumbs []byte
	if a.ThumbnailS3Keys != nil {
		var err error
		thumbs, err = json.Marshal(a.ThumbnailS3Keys)
		if err != nil {
			return fmt.Errorf("failed to marshal thumbnails: %w", err)
		}
	}

	query := `
		INSERT INTO avatars
			(id, user_id, file_name, mime_type, size_bytes, s3_key,
			 thumbnail_s3_keys, upload_status, processing_status)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, err := dbs.storage.Exec(ctx, query,
		a.ID, a.UserID, a.FileName, a.MimeType, a.SizeBytes, a.S3Key,
		thumbs, a.UploadStatus, a.ProcessingStatus,
	)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" { // unique_violation
			return ErrAlreadyInStorage
		}
		return fmt.Errorf("failed to create avatar: %w", err)
	}
	return nil
}

// GetAvatarByID returns an avatar by UUID (excluding soft-deleted).
func (dbs *dbStorage) GetAvatarByID(ctx context.Context, id uuid.UUID) (*models.Avatar, error) {
	if id == uuid.Nil {
		return nil, fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}

	query := fmt.Sprintf(`
		SELECT %s FROM avatars
		WHERE id = $1 AND deleted_at IS NULL
	`, avatarColumns)

	return dbs.scanAvatar(dbs.storage.QueryRow(ctx, query, id))
}

// GetActiveAvatarByUserID returns the most recent non-deleted avatar of the user.
func (dbs *dbStorage) GetActiveAvatarByUserID(ctx context.Context, userID string) (*models.Avatar, error) {
	if userID == "" {
		return nil, fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}

	query := fmt.Sprintf(`
		SELECT %s FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
		LIMIT 1
	`, avatarColumns)

	return dbs.scanAvatar(dbs.storage.QueryRow(ctx, query, userID))
}

// ListAvatarsByUserID returns all non-deleted avatars of the user.
func (dbs *dbStorage) ListAvatarsByUserID(ctx context.Context, userID string) ([]models.Avatar, error) {
	if userID == "" {
		return nil, fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}

	query := fmt.Sprintf(`
		SELECT %s FROM avatars
		WHERE user_id = $1 AND deleted_at IS NULL
		ORDER BY created_at DESC
	`, avatarColumns)

	rows, err := dbs.storage.Query(ctx, query, userID)
	if err != nil {
		return nil, fmt.Errorf("failed to list avatars: %w", err)
	}
	defer rows.Close()

	var result []models.Avatar
	for rows.Next() {
		a, err := dbs.scanAvatar(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, *a)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("rows iteration error: %w", err)
	}
	return result, nil
}

// UpdateUploadStatus updates the upload_status column.
// UpdateUploadStatus updates the upload_status column.
func (dbs *dbStorage) UpdateUploadStatus(
	ctx context.Context,
	id uuid.UUID,
	status models.UploadStatus,
) error {
	if id == uuid.Nil {
		return fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}
	if status == "" {
		return fmt.Errorf("empty status: %w", ErrInvalidArgument)
	}

	tag, err := dbs.storage.Exec(ctx, `
		UPDATE avatars
		SET upload_status = $1, updated_at = NOW()
		WHERE id = $2 AND deleted_at IS NULL
	`, string(status), id)
	if err != nil {
		return fmt.Errorf("failed to update upload status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoChanges
	}
	return nil
}

// UpdateProcessingStatus updates processing_status and thumbnails.
func (dbs *dbStorage) UpdateProcessingStatus(
	ctx context.Context,
	id uuid.UUID,
	status models.ProcessingStatus,
	thumbnails map[string]string,
) error {
	if id == uuid.Nil {
		return fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}
	if status == "" {
		return fmt.Errorf("empty status: %w", ErrInvalidArgument)
	}

	var raw []byte
	if thumbnails != nil {
		var err error
		raw, err = json.Marshal(thumbnails)
		if err != nil {
			return fmt.Errorf("failed to marshal thumbnails: %w", err)
		}
	}

	var tag pgconn.CommandTag
	var err error
	if raw != nil {
		tag, err = dbs.storage.Exec(ctx, `
			UPDATE avatars
			SET processing_status = $1,
			    thumbnail_s3_keys = $2,
			    updated_at = NOW()
			WHERE id = $3 AND deleted_at IS NULL
		`, status, raw, id)
	} else {
		tag, err = dbs.storage.Exec(ctx, `
			UPDATE avatars
			SET processing_status = $1, updated_at = NOW()
			WHERE id = $2 AND deleted_at IS NULL
		`, status, id)
	}
	if err != nil {
		return fmt.Errorf("failed to update processing status: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNoChanges
	}
	return nil
}

// SoftDeleteAvatar marks the avatar as deleted.
// If userID is provided, ownership is verified.
func (dbs *dbStorage) SoftDeleteAvatar(ctx context.Context, id uuid.UUID, userID string) error {
	if id == uuid.Nil {
		return fmt.Errorf("empty avatar id: %w", ErrInvalidArgument)
	}

	query := `
		UPDATE avatars
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE id = $1 AND deleted_at IS NULL
	`
	args := []any{id}
	if userID != "" {
		query += " AND user_id = $2"
		args = append(args, userID)
	}

	tag, err := dbs.storage.Exec(ctx, query, args...)
	if err != nil {
		return fmt.Errorf("failed to soft delete avatar: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAvatarNotFound
	}
	return nil
}

// SoftDeleteActiveUserAvatar soft-deletes the active avatar of the user.
func (dbs *dbStorage) SoftDeleteActiveUserAvatar(ctx context.Context, userID string) error {
	if userID == "" {
		return fmt.Errorf("empty user id: %w", ErrInvalidArgument)
	}

	tag, err := dbs.storage.Exec(ctx, `
		UPDATE avatars
		SET deleted_at = NOW(), updated_at = NOW()
		WHERE user_id = $1 AND deleted_at IS NULL
	`, userID)
	if err != nil {
		return fmt.Errorf("failed to soft delete user avatar: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrAvatarNotFound
	}
	return nil
}

// ========== helpers ==========

// rowScanner is satisfied by both pgx.Row and pgx.Rows.
type rowScanner interface {
	Scan(dest ...any) error
}

func (dbs *dbStorage) scanAvatar(s rowScanner) (*models.Avatar, error) {
	var (
		a         models.Avatar
		rawThumbs []byte
	)

	err := s.Scan(
		&a.ID, &a.UserID, &a.FileName, &a.MimeType, &a.SizeBytes,
		&a.S3Key, &rawThumbs, &a.UploadStatus, &a.ProcessingStatus,
		&a.CreatedAt, &a.UpdatedAt, &a.DeletedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrAvatarNotFound
		}
		return nil, fmt.Errorf("failed to scan avatar: %w", err)
	}

	if len(rawThumbs) > 0 {
		if err := json.Unmarshal(rawThumbs, &a.ThumbnailS3Keys); err != nil {
			return nil, fmt.Errorf("failed to unmarshal thumbnails: %w", err)
		}
	}
	return &a, nil
}
