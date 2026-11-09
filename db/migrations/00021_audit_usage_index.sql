-- +goose Up

-- FEAT-071 phase 1. The usage summary groups a window of the trail by operator
-- and database; with these columns in the index a 30-day summary is an
-- index-only scan rather than a read of every statement's text.
CREATE INDEX audit_entry_usage_idx ON audit_entry (created_at, database)
    INCLUDE (user_id, decision, elapsed_ms, row_count);

-- +goose Down
DROP INDEX audit_entry_usage_idx;
