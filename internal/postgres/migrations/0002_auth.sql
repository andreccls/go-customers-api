CREATE TABLE users (
    id            uuid        PRIMARY KEY,
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    role          text        NOT NULL CHECK (role IN ('admin', 'user')),
    created_at    timestamptz NOT NULL,
    CONSTRAINT users_email_key UNIQUE (email)
);

-- Only the SHA-256 of the refresh token is stored.
CREATE TABLE refresh_tokens (
    token_hash text        PRIMARY KEY,
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    expires_at timestamptz NOT NULL,
    used_at    timestamptz
);

CREATE INDEX refresh_tokens_user_idx ON refresh_tokens (user_id);
