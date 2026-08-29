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
INSERT INTO exercise_attempt (user_id, lesson_slug, code, passed)
VALUES ($1, $2, $3, $4)
RETURNING *;

-- name: ListAttempts :many
SELECT * FROM exercise_attempt
WHERE user_id = $1 AND lesson_slug = $2
ORDER BY created_at DESC
LIMIT $3;
