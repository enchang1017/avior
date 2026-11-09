-- +goose Up
-- FEAT-059 AC-9. What was provisioned, by whom.
--
-- Its own table rather than a row in account_change, which is shaped for a
-- change to an operator: its subject_id references app_user, and a database is
-- not one. Forcing it in would mean either a nullable reference that means
-- something different on half the rows, or a fake account per database.
--
-- Recorded whether or not anything was created. AC-7 makes provisioning
-- something that already exists succeed, and "somebody asked for this and it
-- was already there" is the interesting half of that: two DBAs acting on one
-- preconditions page leave two rows, and the second says it found the first's
-- work rather than saying nothing at all.
CREATE TABLE provision_change (
    id          uuid PRIMARY KEY,
    -- Who asked. Not cascaded from app_user, for the same reason
    -- account_change is not: somebody who leaves does not take the record of
    -- what they allocated with them.
    actor_id    uuid        NOT NULL REFERENCES app_user (id),
    -- database, role, membership.
    kind        text        NOT NULL,
    -- The identifier, exactly as it reached the server.
    name        text        NOT NULL,
    -- False where the object was already there.
    created     boolean     NOT NULL,
    changed_at  timestamptz NOT NULL DEFAULT now(),
    -- 'access' is the grants a new database is given inside itself, which is
    -- the one kind here that is not an object: it has no CREATE of its own, and
    -- it is recorded because what Avior grants a governed database is exactly
    -- the sort of thing somebody comes looking for afterwards.
    CONSTRAINT provision_change_kind_known CHECK (
        kind IN ('database', 'role', 'membership', 'access')
    ),
    CONSTRAINT provision_change_name_present CHECK (name <> '')
);

CREATE INDEX provision_change_changed_idx ON provision_change (changed_at DESC);

-- +goose Down
DROP TABLE provision_change;
