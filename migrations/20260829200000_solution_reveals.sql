-- +goose Up

-- A learner who asked to be shown the answer has not solved the exercise, and a
-- history that cannot tell the two apart is a history worth less than none: the
-- point of recording progress is that a learner can trust what it says.
--
-- Nullable rather than a boolean with a default, because the interesting fact is
-- *when* they gave up rather than merely that they did, and because a null reads
-- unambiguously as "never asked" on every row that predates this column.
ALTER TABLE user_lesson
    ADD COLUMN solution_revealed_at timestamptz;

-- +goose Down

ALTER TABLE user_lesson
    DROP COLUMN solution_revealed_at;
