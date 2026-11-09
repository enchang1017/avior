-- +goose Up
-- FEAT-017 AC-20. What the target's invalid indexes were when a release was
-- applied.
--
-- A CREATE INDEX CONCURRENTLY that fails does not roll back -- it cannot, it ran
-- outside a transaction, which is the whole reason AC-19 exists -- and it leaves
-- an index PostgreSQL has marked invalid. That index takes no part in query
-- planning and is not repaired by re-running the migration, because the name is
-- taken. Somebody has to drop it.
--
-- Recorded on the application rather than read live, and the distinction is the
-- point: this column says what was true when this release ran. Reading it live
-- would answer "what is invalid now", which is a different and much less useful
-- question once six releases have gone past -- nothing would say which one left
-- the debris.
--
-- Written on a SUCCEEDED application as well as a failed one. An index left
-- invalid by an earlier release is not this release's fault and is not a reason
-- to withhold it, but this is the moment somebody is looking.
--
-- Empty array rather than NULL for "none found", so a reader never has to decide
-- what NULL meant. "Could not be read" is not represented at all: an error
-- reading a diagnostic does not turn a successful apply into a failed one, so it
-- lands as an empty array and the log carries the reason.
ALTER TABLE release_application
    ADD COLUMN invalid_indexes text[] NOT NULL DEFAULT '{}';

-- +goose Down
ALTER TABLE release_application
    DROP COLUMN invalid_indexes;
