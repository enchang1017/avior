-- +goose Up
-- FEAT-058. Which roles reach which governed database.
--
-- Deny by default: a role reaches a database because a row says so. The other
-- direction fails the wrong way — a service database appears on this server
-- every time a service is added, and forgetting to hide one exposes it to
-- everybody, while forgetting to grant one is fixed in a minute by whoever
-- asks.
--
-- There is deliberately no migration granting what used to be visible.
-- Preserving "everybody saw everything" would ship the default this table
-- exists to remove; a deployment configures it before it is used.
CREATE TABLE database_grant (
    id         uuid PRIMARY KEY,
    role       text NOT NULL,
    database   text NOT NULL,
    reason     text        NOT NULL DEFAULT '',
    granted_by uuid        REFERENCES app_user (id),
    granted_at timestamptz NOT NULL DEFAULT now(),
    UNIQUE (role, database)
);

CREATE INDEX database_grant_role_idx ON database_grant (role);

-- Who opened or closed a door onto a production database, and when.
--
-- Append-only, for the reason account_change is: access to production is the
-- thing this system governs, and a record of who granted it is worth nothing if
-- it can be revised afterwards. A change with no actor is the deployment's seed
-- rather than a person, the same convention FEAT-056 uses.
CREATE TABLE database_grant_change (
    id         uuid PRIMARY KEY,
    actor_id   uuid        REFERENCES app_user (id),
    role       text        NOT NULL,
    database   text        NOT NULL,
    action     text        NOT NULL,
    reason     text        NOT NULL DEFAULT '',
    changed_at timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT database_grant_action_known CHECK (action IN ('grant', 'revoke'))
);

CREATE INDEX database_grant_change_idx ON database_grant_change (database, changed_at DESC);

CREATE TRIGGER database_grant_change_is_append_only
    BEFORE UPDATE OR DELETE ON database_grant_change
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

-- +goose Down
DROP TRIGGER database_grant_change_is_append_only ON database_grant_change;
DROP TABLE database_grant_change;
DROP TABLE database_grant;
