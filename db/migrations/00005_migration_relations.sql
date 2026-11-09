-- +goose Up
-- FEAT-039. The relations each migration names, recorded when it is submitted.
--
-- NULL is not an empty list. NULL means the extractor could not account for the
-- whole file — a DO block, a statement built by format(), a shape it has not
-- been taught — and AC-8 puts such a file into every table filter. An empty
-- array means the file was read and names no relation. Collapsing the two would
-- make an unreadable migration disappear from the filter it belongs in, which is
-- the one failure this column exists to prevent.
--
-- Every row that predates this column is NULL, which is the correct answer for
-- them: nothing parsed them, so nothing knows.
ALTER TABLE release_migration ADD COLUMN relations text[];

-- +goose Down
ALTER TABLE release_migration DROP COLUMN relations;
