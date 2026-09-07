-- Every query here is scoped by user_id. That scoping is the access control:
-- there is no second check elsewhere that a row belongs to the caller, so a
-- query that omits it is a data leak rather than a bug in a filter.

-- name: UpsertLessonProgress :one
INSERT INTO user_lesson (user_id, lesson_slug, status, completed_at)
VALUES ($1, $2, $3, $4)
ON CONFLICT (user_id, lesson_slug) DO UPDATE
-- Completion is sticky. Re-opening a finished lesson is an ordinary thing to
-- do, and writing EXCLUDED.status unconditionally would demote it to
-- in_progress while COALESCE below still kept its completed_at — a pair the
-- table's CHECK constraint rejects outright, so this was a runtime error and
-- not merely wrong data.
SET status       = CASE
                       WHEN user_lesson.status = 'completed' THEN 'completed'
                       ELSE EXCLUDED.status
                   END,
    -- COALESCE so re-completing keeps the original timestamp: the interesting
    -- fact is when it was first finished, not most recently.
    completed_at = COALESCE(user_lesson.completed_at, EXCLUDED.completed_at)
RETURNING *;

-- name: GetLessonProgress :one
SELECT * FROM user_lesson
WHERE user_id = $1 AND lesson_slug = $2;

-- name: ListProgressForUser :many
SELECT * FROM user_lesson
WHERE user_id = $1
ORDER BY lesson_slug;

-- name: RecordAttempt :one
-- Provenance is passed in rather than derived here: only the caller knows
-- whether this arrived as a form post from an editor or as a tool call from an
-- agent, and that difference is the whole value of the column.
INSERT INTO exercise_attempt (user_id, lesson_slug, code, passed, provenance)
VALUES ($1, $2, $3, $4, $5)
RETURNING *;

-- name: ListAttempts :many
SELECT * FROM exercise_attempt
WHERE user_id = $1 AND lesson_slug = $2
ORDER BY created_at DESC
LIMIT $3;

-- name: CountFailedAttempts :one
-- Drives when a hint and then the solution are offered. Counted rather than
-- listed because the handler needs the number and nothing else, and the code
-- column on these rows is the largest thing in the table.
SELECT count(*) FROM exercise_attempt
WHERE user_id = $1 AND lesson_slug = $2 AND passed = false;

-- name: MarkSolutionRevealed :exec
-- An upsert rather than an update: a learner reaches this having submitted, and
-- a submission does not require ever having opened the lesson page that creates
-- the row. COALESCE keeps the first reveal, because "when did they give up on
-- this" is asked of the first time, not the most recent.
INSERT INTO user_lesson (user_id, lesson_slug, status, solution_revealed_at)
VALUES ($1, $2, 'in_progress', now())
ON CONFLICT (user_id, lesson_slug) DO UPDATE
SET solution_revealed_at = COALESCE(user_lesson.solution_revealed_at, now());
