-- +goose Up

-- What is worth knowing about a submission is no longer only whether it passed.
-- An agent can write correct Swift on a learner's behalf, and a history that
-- cannot tell that apart from a learner writing it is a history that quietly
-- overstates what they can do.
--
-- Over MCP this is a fact rather than a guess: the agent authenticated as the
-- learner and called the tool, so 'agent' is recorded with certainty. In the
-- browser it is a heuristic the editor derives from paste size, which is why
-- the middle values exist at all — 'mixed' is an honest answer where 'typed'
-- and 'pasted' would both be wrong.
--
-- A default rather than a nullable column, because 'unknown' is a real answer
-- and the one every row predating this column deserves. The CHECK is here for
-- the same reason every other one in this schema is: the set is small, closed,
-- and worth more to the database than to a comment.
--
-- This column labels. It is never read to refuse, delay, or differently grade a
-- submission, and it is never shown to a learner as a judgement on their work.
ALTER TABLE exercise_attempt
    ADD COLUMN provenance text NOT NULL DEFAULT 'unknown'
        CHECK (provenance IN ('unknown', 'typed', 'mixed', 'pasted', 'agent'));

-- +goose Down

ALTER TABLE exercise_attempt
    DROP COLUMN provenance;
