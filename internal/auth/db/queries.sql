-- name: CreateUser :one
INSERT INTO users (email, password_hash)
VALUES ($1, $2)
RETURNING *;

-- Addresses are compared case-insensitively; the unique index is on
-- lower(email), so this predicate is what makes the lookup use it.
-- name: GetUserByEmail :one
SELECT * FROM users
WHERE lower(email) = lower(sqlc.arg(email)::text);

-- name: GetUserByID :one
SELECT * FROM users
WHERE id = $1;

-- name: CreateSession :one
INSERT INTO sessions (user_id, token_hash, expires_at, user_agent, ip)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- Returns the session and its user together: every authenticated request needs
-- both, and two round trips per request to assemble them is one too many.
-- Expired rows are excluded here rather than deleted on read, so the lookup
-- stays a pure read and cleanup can happen on its own schedule.
-- name: GetSessionWithUser :one
SELECT
    sqlc.embed(sessions),
    sqlc.embed(users)
FROM sessions
JOIN users ON users.id = sessions.user_id
WHERE sessions.token_hash = $1
  AND sessions.expires_at > now();

-- name: TouchSession :exec
UPDATE sessions
SET last_used_at = now(),
    expires_at   = $2
WHERE id = $1;

-- name: DeleteSession :exec
DELETE FROM sessions
WHERE token_hash = $1;

-- name: DeleteSessionsForUser :exec
DELETE FROM sessions
WHERE user_id = $1;

-- name: DeleteExpiredSessions :execrows
DELETE FROM sessions
WHERE expires_at <= now();
