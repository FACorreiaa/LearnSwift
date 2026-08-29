-- +goose Up

CREATE TABLE user_lesson (
    user_id      uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    -- The lesson's slug, not a foreign key. Lessons are markdown files embedded
    -- in the binary, not rows, so there is no table to point at. A slug that no
    -- longer exists is a stale progress row, which is harmless and is filtered
    -- out when progress is joined against the lesson index at read time.
    lesson_slug  text        NOT NULL,
    -- text + CHECK rather than an enum: adding a state later is an ordinary
    -- migration that can also be rolled back.
    status       text        NOT NULL CHECK (status IN ('in_progress', 'completed')),
    started_at   timestamptz NOT NULL DEFAULT now(),
    completed_at timestamptz,

    PRIMARY KEY (user_id, lesson_slug),

    -- Keeps the two columns from disagreeing. A row cannot claim to be
    -- completed with no timestamp, and cannot carry one while in progress.
    CONSTRAINT user_lesson_completed_at_matches_status CHECK (
        (status = 'completed' AND completed_at IS NOT NULL)
        OR (status = 'in_progress' AND completed_at IS NULL)
    )
);

CREATE INDEX user_lesson_user_id_idx ON user_lesson (user_id);

CREATE TABLE exercise_attempt (
    id          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id     uuid        NOT NULL REFERENCES users (id) ON DELETE CASCADE,
    lesson_slug text        NOT NULL,
    code        text        NOT NULL,
    passed      boolean     NOT NULL,
    created_at  timestamptz NOT NULL DEFAULT now()
);

-- Attempts are read newest-first for one user and one lesson, which is exactly
-- this index.
CREATE INDEX exercise_attempt_user_lesson_idx
    ON exercise_attempt (user_id, lesson_slug, created_at DESC);

-- +goose Down

DROP TABLE exercise_attempt;
DROP TABLE user_lesson;
