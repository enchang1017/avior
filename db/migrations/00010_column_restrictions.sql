-- +goose Up
-- FEAT-055. A restricted column is refused to everybody until somebody unlocks
-- it for a role and says why.
--
-- The opposite default from column_denial, which this supersedes: there, a
-- column nobody denied was readable. Here a column somebody was careful enough
-- to mark is refused to every role, including dba and administrator, and
-- becomes readable only through a row with somebody's name on it.
CREATE TABLE column_restriction (
    id          uuid PRIMARY KEY,
    database    text NOT NULL,
    object_name text NOT NULL,
    column_name text NOT NULL,
    reason      text        NOT NULL DEFAULT '',
    created_by  uuid        REFERENCES app_user (id),
    created_at  timestamptz NOT NULL DEFAULT now(),
    UNIQUE (database, object_name, column_name)
);

-- A role may read one anyway.
--
-- Separate from the restriction because they are two decisions by two people at
-- two times: one says this column is not ordinary, the other says this role may
-- read it and here is why. Folded into one table with a nullable role, the same
-- fact would be two rows meaning different things — and "restricted, and nobody
-- may read it" would have no representation at all.
CREATE TABLE column_unlock (
    id          uuid PRIMARY KEY,
    database    text NOT NULL,
    object_name text NOT NULL,
    column_name text NOT NULL,
    role        text NOT NULL,
    -- Required. Restricting is the careful default and needs no defence;
    -- unlocking is the exception somebody has to justify afterwards.
    reason      text        NOT NULL,
    granted_by  uuid        REFERENCES app_user (id),
    granted_at  timestamptz NOT NULL DEFAULT now(),
    -- Null lasts until somebody removes it. A time makes it stop on its own,
    -- which is what "temporary" has to mean if it is to mean anything.
    expires_at  timestamptz,
    UNIQUE (database, object_name, column_name, role)
);

CREATE INDEX column_unlock_lookup_idx
    ON column_unlock (database, role);

-- Who restricted or unlocked what. Append-only, beside the account and grant
-- records, for the same reason: a column somebody could read is worth knowing
-- about afterwards, and a record that can be revised is not worth keeping.
CREATE TABLE column_change (
    id          uuid PRIMARY KEY,
    actor_id    uuid        REFERENCES app_user (id),
    database    text        NOT NULL,
    object_name text        NOT NULL,
    column_name text        NOT NULL,
    role        text        NOT NULL DEFAULT '',
    action      text        NOT NULL,
    reason      text        NOT NULL DEFAULT '',
    expires_at  timestamptz,
    changed_at  timestamptz NOT NULL DEFAULT now(),
    CONSTRAINT column_change_action_known CHECK (
        action IN ('restrict', 'unrestrict', 'unlock', 'lock')
    ),
    -- An unlock and a lock name the role they are about; a restriction is about
    -- the column and names none.
    CONSTRAINT column_change_role_present CHECK (
        (action IN ('unlock', 'lock')) = (role <> '')
    )
);

CREATE INDEX column_change_idx ON column_change (database, object_name, changed_at DESC);

CREATE TRIGGER column_change_is_append_only
    BEFORE UPDATE OR DELETE ON column_change
    FOR EACH ROW EXECUTE FUNCTION refuse_mutation();

-- Every column any role was denied becomes restricted, and nothing is unlocked.
--
-- The roles that were not denied lose access they had. That is deliberate: the
-- new default is stricter, and a migration that unlocked those roles to preserve
-- what they could read would be the one thing this card must not do — widen
-- access quietly while claiming to tighten it. Whoever needs one back asks, and
-- the asking is recorded.
INSERT INTO column_restriction (id, database, object_name, column_name, reason)
SELECT DISTINCT ON (database, object_name, column_name)
       gen_random_uuid(), database, object_name, column_name,
       'carried over from a column denial'
FROM column_denial
ON CONFLICT (database, object_name, column_name) DO NOTHING;

DROP TABLE column_denial;

-- +goose Down
CREATE TABLE column_denial (
    id          uuid PRIMARY KEY,
    role        text NOT NULL,
    database    text NOT NULL,
    object_name text NOT NULL,
    column_name text NOT NULL,
    reason      text NOT NULL DEFAULT '',
    UNIQUE (role, database, object_name, column_name)
);

DROP TRIGGER column_change_is_append_only ON column_change;
DROP TABLE column_change;
DROP INDEX column_unlock_lookup_idx;
DROP TABLE column_unlock;
DROP TABLE column_restriction;
