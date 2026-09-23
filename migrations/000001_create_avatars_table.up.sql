CREATE EXTENSION IF NOT EXISTS "pgcrypto";

CREATE TABLE IF NOT EXISTS avatars (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
    user_id VARCHAR(255) NOT NULL,
    file_name VARCHAR(255) NOT NULL,
    mime_type VARCHAR(100) NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0),
    s3_key VARCHAR(500) NOT NULL,
    thumbnail_s3_keys JSONB NOT NULL DEFAULT '{}'::jsonb,
    upload_status VARCHAR(50) NOT NULL DEFAULT 'uploading',
    processing_status VARCHAR(50) NOT NULL DEFAULT 'pending',
    created_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE
);

-- Sanity: statuses must be within the known set.
-- Using CHECK instead of ENUM so we can extend it later without a new type.
ALTER TABLE avatars
ADD CONSTRAINT chk_avatars_upload_status CHECK (
    upload_status IN (
        'uploading',
        'uploaded',
        'failed'
    )
);

ALTER TABLE avatars
ADD CONSTRAINT chk_avatars_processing_status CHECK (
    processing_status IN (
        'pending',
        'processing',
        'completed',
        'failed'
    )
);