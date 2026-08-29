-- +goose Up

CREATE TABLE users (
    id            uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    email         text        NOT NULL,
    password_hash text        NOT NULL,
    created_at    timestamptz NOT NULL DEFAULT now(),
    updated_at    timestamptz NOT NULL DEFAULT now()
);

-- Addresses are compared case-insensitively, but stored as the visitor typed
-- them: a unique index over lower(email) enforces the former without the
-- citext extension and without rewriting anybody's capitalisation.
CREATE UNIQUE INDEX users_email_lower_key ON users (lower(email));

CREATE TABLE sessions (
    id         uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id    uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The SHA-256 of the session token, never the token. A leaked database
    -- backup then yields no usable session: the digest cannot be replayed as
    -- a cookie. Plain SHA-256 rather than a password hash is correct here
    -- because the token is already 32 bytes of uniform randomness, so there
    -- is no low-entropy guess for a slow hash to defend against — and this
    -- runs on every single request.
    token_hash bytea       NOT NULL,
    created_at timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at timestamptz NOT NULL,
    user_agent text,
    ip         inet
);

CREATE UNIQUE INDEX sessions_token_hash_key ON sessions (token_hash);
CREATE INDEX sessions_user_id_idx ON sessions (user_id);
-- Supports the sweep that deletes expired rows.
CREATE INDEX sessions_expires_at_idx ON sessions (expires_at);

-- +goose Down

DROP TABLE sessions;
DROP TABLE users;
