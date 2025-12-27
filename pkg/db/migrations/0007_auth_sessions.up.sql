-- +goose Up
-- +goose StatementBegin

-- User sessions table for refresh token management
-- This is used by the auth repository for JWT refresh token sessions
CREATE TABLE IF NOT EXISTS user_sessions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid (),
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    hashed_refresh_token VARCHAR(255) NOT NULL,
    user_agent TEXT,
    client_ip TEXT,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMP
    WITH
        TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
        UNIQUE (hashed_refresh_token)
);

-- Index for efficient token lookups
CREATE INDEX IF NOT EXISTS idx_user_sessions_hashed_token ON user_sessions (hashed_refresh_token);

CREATE INDEX IF NOT EXISTS idx_user_sessions_user_id ON user_sessions (user_id);

CREATE INDEX IF NOT EXISTS idx_user_sessions_expires_at ON user_sessions (expires_at);

-- User tokens table for verification/reset tokens
CREATE TABLE IF NOT EXISTS user_tokens (
    token_hash VARCHAR(255) PRIMARY KEY,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    type VARCHAR(50) NOT NULL, -- 'email_verification', 'password_reset', etc.
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMP
    WITH
        TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_user_tokens_user_id ON user_tokens (user_id);

CREATE INDEX IF NOT EXISTS idx_user_tokens_type ON user_tokens(type);

-- OAuth identities table for social logins
CREATE TABLE IF NOT EXISTS user_oauth_identities (
    provider_name VARCHAR(50) NOT NULL,
    provider_user_id VARCHAR(255) NOT NULL,
    user_id UUID NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    provider_access_token TEXT,
    provider_refresh_token TEXT,
    created_at TIMESTAMP
    WITH
        TIME ZONE NOT NULL DEFAULT CURRENT_TIMESTAMP,
        updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
        PRIMARY KEY (
            provider_name,
            provider_user_id
        )
);

CREATE INDEX IF NOT EXISTS idx_oauth_identities_user_id ON user_oauth_identities (user_id);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_oauth_identities;

DROP TABLE IF EXISTS user_tokens;

DROP TABLE IF EXISTS user_sessions;
-- +goose StatementEnd