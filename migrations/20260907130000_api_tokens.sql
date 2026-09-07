-- +goose Up

-- A browser proves who it is with a cookie. An agent cannot: it has no cookie
-- jar, it does not follow redirects to a login form, and the CSRF double-submit
-- that protects every browser route is meaningless to a client that was never
-- tricked into making a request it did not intend.
--
-- So a second credential, deliberately shaped like the first. Only the digest
-- of the token is stored, exactly as for sessions, so a leaked backup yields
-- nothing that can be replayed. The plaintext exists once, in the response that
-- created it, and is never recoverable afterwards.
--
-- The label is the one field here for the learner rather than the server: two
-- tokens with no names are two tokens nobody can safely revoke.
CREATE TABLE api_token (
    id           uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    token_hash   bytea       NOT NULL,
    label        text        NOT NULL,
    created_at   timestamptz NOT NULL DEFAULT now(),
    last_used_at timestamptz NOT NULL DEFAULT now(),
    expires_at   timestamptz NOT NULL
);

-- The lookup on every authenticated request is by digest, and it must be unique
-- for the same reason a session's is: two rows matching one credential is an
-- ambiguity with no correct resolution.
CREATE UNIQUE INDEX api_token_token_hash_key ON api_token (token_hash);

-- Listing a learner's own tokens, which is the whole of the management page.
CREATE INDEX api_token_user_id_idx ON api_token (user_id);

-- The sweep, mirroring the one on sessions: the lookup query excludes expired
-- rows rather than deleting them, so something has to remove them eventually.
CREATE INDEX api_token_expires_at_idx ON api_token (expires_at);

-- +goose Down

DROP TABLE api_token;
