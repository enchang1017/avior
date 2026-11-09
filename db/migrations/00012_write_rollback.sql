-- +goose Up
-- FEAT-009. Rolling a write back, as a write of its own.
--
-- A rollback is not a flag on the request it undoes. It is another change to a
-- production table, so it is another row here: its own statement, its own
-- capture, its own reverse statement, and its own line in the record. That is
-- what makes the rollback of a rollback an ordinary thing rather than a
-- question nobody thought about.
ALTER TABLE write_request
    ADD COLUMN rollback_of uuid REFERENCES write_request (id);

CREATE INDEX write_request_rollback_idx ON write_request (rollback_of)
    WHERE rollback_of IS NOT NULL;

-- One request is undone at most once, and by at most one rollback that ran.
--
-- A rollback that failed leaves its row and does not hold the place: the change
-- did not happen, so somebody has to be able to try again. A rollback that
-- succeeded does hold it — a second one would put the same rows back over
-- whatever the first restored, which is not undoing anything.
CREATE UNIQUE INDEX write_request_one_rollback_idx ON write_request (rollback_of)
    WHERE rollback_of IS NOT NULL AND state = 'applied';

-- A rollback cannot undo itself, directly or by being its own parent.
ALTER TABLE write_request
    ADD CONSTRAINT write_request_rollback_is_another CHECK (rollback_of IS DISTINCT FROM id);

-- +goose StatementBegin
-- rollback_of is part of what was submitted, so it joins the columns a request
-- cannot have rewritten after the fact.
CREATE OR REPLACE FUNCTION refuse_write_request_rewrite() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF NEW.id IS DISTINCT FROM OLD.id
        OR NEW.database IS DISTINCT FROM OLD.database
        OR NEW.statement IS DISTINCT FROM OLD.statement
        OR NEW.kind IS DISTINCT FROM OLD.kind
        OR NEW.target_table IS DISTINCT FROM OLD.target_table
        OR NEW.reason IS DISTINCT FROM OLD.reason
        OR NEW.requested_by IS DISTINCT FROM OLD.requested_by
        OR NEW.requested_at IS DISTINCT FROM OLD.requested_at
        OR NEW.estimated_rows IS DISTINCT FROM OLD.estimated_rows
        OR NEW.rollback_of IS DISTINCT FROM OLD.rollback_of
    THEN
        RAISE EXCEPTION
            'a submitted write request cannot be rewritten: only its outcome may change'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

ALTER TABLE write_request_change
    DROP CONSTRAINT write_request_action_known;

ALTER TABLE write_request_change
    ADD CONSTRAINT write_request_action_known CHECK (
        action IN ('submit', 'approve', 'reject', 'withdraw', 'run', 'fail',
                   'capture_removed', 'rollback')
    );

-- +goose Down
ALTER TABLE write_request_change
    DROP CONSTRAINT write_request_action_known;

ALTER TABLE write_request_change
    ADD CONSTRAINT write_request_action_known CHECK (
        action IN ('submit', 'approve', 'reject', 'withdraw', 'run', 'fail', 'capture_removed')
    );

ALTER TABLE write_request DROP CONSTRAINT write_request_rollback_is_another;
DROP INDEX write_request_one_rollback_idx;
DROP INDEX write_request_rollback_idx;
ALTER TABLE write_request DROP COLUMN rollback_of;
