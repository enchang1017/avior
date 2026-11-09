-- +goose Up
-- FEAT-009. A developer asks for a row change; an approver reads what it would
-- touch and lets it run or refuses it.
--
-- The rows needed to reverse the change do not live here. They are captured in
-- the governed database, in the same transaction as the write, because
-- PostgreSQL has no transaction spanning two databases and a backup one
-- database away would need two-phase commit — reopening the window the capture
-- exists to close. What lives here is the request, the decision, and where the
-- capture went.
CREATE TABLE write_request (
    id             uuid PRIMARY KEY,
    database       text NOT NULL,
    -- As submitted. What an approver read has to be what runs.
    statement      text NOT NULL,
    kind           text NOT NULL,
    -- Schema-qualified, as the execution plan resolved it rather than as the
    -- text spelled it: the search path can put a bare name somewhere the
    -- reader did not expect.
    target_table   text NOT NULL,
    reason         text NOT NULL,
    requested_by   uuid        NOT NULL REFERENCES app_user (id),
    requested_at   timestamptz NOT NULL DEFAULT now(),
    -- What the optimiser said before anybody approved it. Kept beside
    -- affected_rows on purpose: an estimate of ten and an outcome of nine
    -- hundred is the thing somebody wants to find later. AC-8.
    estimated_rows bigint      NOT NULL,
    state          text        NOT NULL,

    decided_by     uuid REFERENCES app_user (id),
    decided_at     timestamptz,
    decision_note  text NOT NULL DEFAULT '',

    ran_at         timestamptz,
    affected_rows  bigint,
    duration_ms    integer,
    -- Empty unless the statement failed. A request that failed reads as failed
    -- rather than as still waiting. AC-7.
    error          text NOT NULL DEFAULT '',

    -- How many rows the capture holds, and the statement that puts them back.
    -- Null capture_rows means nothing was captured, because nothing ran.
    capture_rows       bigint,
    reverse_statement  text NOT NULL DEFAULT '',
    -- Set when a DBA removes the capture. The request keeps its record and
    -- reads afterwards as no longer reversible. AC-17.
    capture_removed_at timestamptz,
    capture_removed_by uuid REFERENCES app_user (id),

    CONSTRAINT write_request_kind_known CHECK (kind IN ('insert', 'update', 'delete')),
    -- There is no resting `approved` state. Approving runs the statement in the
    -- same operation, so a request is waiting, refused, withdrawn, or it ran.
    -- A state meaning "somebody said yes and nothing happened" would be a state
    -- nothing could get out of.
    CONSTRAINT write_request_state_known CHECK (
        state IN ('pending', 'rejected', 'withdrawn', 'applied', 'failed')
    ),
    -- A decision is a person and a time or it is neither.
    CONSTRAINT write_request_decision_whole CHECK (
        (decided_by IS NULL) = (decided_at IS NULL)
    ),
    CONSTRAINT write_request_capture_removal_whole CHECK (
        (capture_removed_by IS NULL) = (capture_removed_at IS NULL)
    )
);

CREATE INDEX write_request_state_idx ON write_request (state, requested_at DESC);
CREATE INDEX write_request_requester_idx ON write_request (requested_by, requested_at DESC);
-- The capture listing reads by age across every database, so the age is what it
-- is ordered by. A partial index, because a request whose capture is gone or
-- was never taken is not on that page at all.
CREATE INDEX write_request_capture_idx ON write_request (ran_at DESC)
    WHERE capture_rows IS NOT NULL AND capture_removed_at IS NULL;

-- Every step: submitted, approved, refused, withdrawn, run, and the removal of
-- the capture. Append-only, beside the release and account records, because an
-- audit reads these when somebody is asking what actually happened — which is
-- exactly when a revised row does the most damage.
CREATE TABLE write_request_change (
    id         uuid PRIMARY KEY,
    request_id uuid        NOT NULL REFERENCES write_request (id),
    -- Null when nobody did it: a deployment running without approval, which is
    -- the same convention the other records use.
    actor_id   uuid REFERENCES app_user (id),
    action     text        NOT NULL,
    note       text        NOT NULL DEFAULT '',
    changed_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT write_request_action_known CHECK (
        action IN ('submit', 'approve', 'reject', 'withdraw', 'run', 'fail', 'capture_removed')
    )
);

CREATE INDEX write_request_change_idx
    ON write_request_change (request_id, changed_at DESC);

CREATE TRIGGER write_request_change_is_append_only
    BEFORE UPDATE OR DELETE ON write_request_change
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

-- +goose StatementBegin
-- What was submitted cannot be rewritten. The state and the outcome are the
-- legitimate mutations; the statement, the target and who asked are statements
-- about something that already happened.
CREATE FUNCTION refuse_write_request_rewrite() RETURNS trigger
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
    THEN
        RAISE EXCEPTION
            'a submitted write request cannot be rewritten: only its outcome may change'
            USING ERRCODE = 'restrict_violation';
    END IF;
    RETURN NEW;
END;
$$;
-- +goose StatementEnd

CREATE TRIGGER write_request_only_outcome_changes
    BEFORE UPDATE ON write_request
    FOR EACH ROW EXECUTE FUNCTION refuse_write_request_rewrite();

CREATE TRIGGER write_request_is_never_deleted
    BEFORE DELETE ON write_request
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

-- +goose Down
DROP TRIGGER write_request_is_never_deleted ON write_request;
DROP TRIGGER write_request_only_outcome_changes ON write_request;
DROP FUNCTION refuse_write_request_rewrite();
DROP TRIGGER write_request_change_is_append_only ON write_request_change;
DROP TABLE write_request_change;
DROP TABLE write_request;
