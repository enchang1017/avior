-- +goose Up
ALTER TABLE write_request ADD COLUMN expected_affected_rows bigint
    CHECK (expected_affected_rows IS NULL OR expected_affected_rows = 1);

-- +goose Down
ALTER TABLE write_request DROP COLUMN expected_affected_rows;
