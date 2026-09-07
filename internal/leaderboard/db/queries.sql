-- Queries for the two boards.
--
-- Only learners with a row in leaderboard_optin appear anywhere here. That is
-- not a filter applied on top of the boards — it is the join they start from,
-- so there is no ordering of this file in which a learner who never opted in
-- can be listed.

-- name: OptIn :one
-- Upsert rather than insert, so changing a display name is the same action as
-- choosing one and needs no separate route.
INSERT INTO leaderboard_optin (user_id, display_name)
VALUES ($1, $2)
ON CONFLICT (user_id) DO UPDATE
SET display_name = excluded.display_name
RETURNING *;

-- name: OptOut :execrows
DELETE FROM leaderboard_optin
WHERE user_id = $1;

-- name: GetOptIn :one
SELECT * FROM leaderboard_optin
WHERE user_id = $1;

-- name: Board :many
-- Both counts for every opted-in learner, in one pass.
--
-- first_pass reduces each learner's attempts to the *first* one that passed per
-- lesson, which is the only attempt whose provenance means anything: a learner
-- who solves a lesson unaided and later pastes the same answer back in has not
-- retroactively cheated, and one who pastes it first has not earned the solo
-- count by retyping it afterwards.
--
-- Solo requires two things and it is worth being explicit about both. The first
-- passing attempt was typed, and the answer was never revealed — a lesson
-- finished after reading the solution is still finished, but it is not solved
-- unaided.
--
-- Everything else that passed is assisted, which includes every submission that
-- arrived over MCP.
WITH first_pass AS (
    SELECT DISTINCT ON (user_id, lesson_slug)
        user_id, lesson_slug, provenance
    FROM exercise_attempt
    WHERE passed
    ORDER BY user_id, lesson_slug, created_at
)
SELECT
    o.display_name,
    count(*) FILTER (
        WHERE fp.provenance = 'typed' AND ul.solution_revealed_at IS NULL
    )::bigint AS solo,
    count(*) FILTER (
        WHERE NOT (fp.provenance = 'typed' AND ul.solution_revealed_at IS NULL)
    )::bigint AS assisted
FROM leaderboard_optin o
LEFT JOIN first_pass fp ON fp.user_id = o.user_id
LEFT JOIN user_lesson ul
       ON ul.user_id = fp.user_id AND ul.lesson_slug = fp.lesson_slug
GROUP BY o.user_id, o.display_name
ORDER BY solo DESC, assisted DESC, o.display_name
LIMIT $1;
