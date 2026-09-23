CREATE INDEX IF NOT EXISTS idx_avatars_user_id ON avatars (user_id)
WHERE
    deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_avatars_status ON avatars (
    upload_status,
    processing_status
)
WHERE
    deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_avatars_user_created_at ON avatars (user_id, created_at DESC)
WHERE
    deleted_at IS NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_avatars_s3_key ON avatars (s3_key);