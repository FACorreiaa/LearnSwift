-- +goose Up

-- Two boards, and the reason there are two is the whole feature: an agent can
-- write correct Swift, so "solved forty lessons" and "solved forty lessons
-- unaided" have to be different numbers or the first one means nothing.
--
-- A row here is consent, so absence of a row is the default and opting out is a
-- delete. Kept out of the users table for exactly that reason: a column would
-- make every account carry a leaderboard state whether or not it wanted one,
-- and "opted out" and "never asked" would become the same value.
--
-- The display name is the only thing shown. It is not the email address, and
-- deliberately: a learner comparing solve counts with strangers has not agreed
-- to publish who they are.
CREATE TABLE leaderboard_optin (
    user_id      uuid        PRIMARY KEY REFERENCES users (id) ON DELETE CASCADE,
    display_name text        NOT NULL,
    opted_in_at  timestamptz NOT NULL DEFAULT now()
);

-- Case-insensitively unique, following the rule already used for email: two
-- learners distinguishable only by capitalisation is an impersonation waiting
-- to happen on a page whose entire content is names.
CREATE UNIQUE INDEX leaderboard_optin_display_name_key
    ON leaderboard_optin (lower(display_name));

-- +goose Down

DROP TABLE leaderboard_optin;
