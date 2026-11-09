-- name: GetInstance :one
SELECT id, name, kind, host, port, disabled FROM db_instance WHERE id = $1;

-- name: RegisterInstance :exec
-- The governed server this deployment was configured to reach, recorded so that
-- an audit row can reference it.
--
-- Every submitted statement is audited before it runs and the audit row names an
-- instance, so a deployment with no row here cannot answer a single query — it
-- fails on the foreign key, which reads as the gateway being broken rather than
-- as nobody having registered the server it was pointed at. Until FEAT-012 makes
-- this something a DBA does on a page, the deployment registers what it was
-- given.
--
-- The name and address are updated because they are describing the same
-- configured server: a deployment moved to another host is still that
-- deployment, and leaving the old address here would make the audit trail name
-- somewhere the statement did not run.
INSERT INTO db_instance (id, name, kind, host, port)
VALUES ($1, $2, 'postgres', $3, $4)
ON CONFLICT (id) DO UPDATE
SET name = EXCLUDED.name, host = EXCLUDED.host, port = EXCLUDED.port;

-- name: ListDeniedColumns :many
-- FEAT-055. Restricted, minus what these roles have been unlocked for.
--
-- The default inverted here: a column nobody marked is readable, and a column
-- somebody marked is refused to every role until a row unlocks it. So this is a
-- subtraction rather than a lookup, and the shape it returns is unchanged — the
-- guard asks the same question and gets the same answer type.
--
-- The expiry is compared against the database's clock rather than the
-- application's. An unlock that ends is a security boundary, and two clocks
-- disagreeing about when is not a thing to leave to chance.
SELECT r.object_name, r.column_name
FROM column_restriction r
WHERE r.database = $1
  AND NOT EXISTS (
      SELECT 1
      FROM column_unlock u
      WHERE u.database = r.database
        AND u.object_name = r.object_name
        AND u.column_name = r.column_name
        AND u.role = ANY(@roles::text[])
        AND (u.expires_at IS NULL OR u.expires_at > now())
  )
ORDER BY r.object_name, r.column_name;

-- name: ListColumnRestrictions :many
-- Every restricted column, for the page. Readable by anybody signed in: knowing
-- a column is restricted is what stops a refusal reading as a fault, and it is
-- learned from one refusal anyway.
SELECT r.database, r.object_name, r.column_name, r.reason, r.created_at
FROM column_restriction r
ORDER BY r.database, r.object_name, r.column_name;

-- name: ListColumnUnlocks :many
SELECT u.database, u.object_name, u.column_name, u.role, u.reason,
       u.granted_at, u.expires_at,
       COALESCE(g.username, '')::text AS granted_by
FROM column_unlock u
LEFT JOIN app_user g ON g.id = u.granted_by
ORDER BY u.database, u.object_name, u.column_name, u.role;

-- name: RestrictColumn :execrows
INSERT INTO column_restriction (id, database, object_name, column_name, reason, created_by)
VALUES ($1, $2, $3, $4, $5, $6)
ON CONFLICT (database, object_name, column_name) DO NOTHING;

-- name: UnrestrictColumn :execrows
-- The unlocks that named it are left where they are: they are inert without a
-- restriction, and deleting them would delete the record of who was given what.
DELETE FROM column_restriction
WHERE database = $1 AND object_name = $2 AND column_name = $3;

-- name: ColumnIsRestricted :one
SELECT EXISTS (
    SELECT 1 FROM column_restriction
    WHERE database = $1 AND object_name = $2 AND column_name = $3
);

-- name: UnlockColumn :execrows
INSERT INTO column_unlock (
    id, database, object_name, column_name, role, reason, granted_by, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (database, object_name, column_name, role) DO UPDATE
    SET reason = EXCLUDED.reason,
        granted_by = EXCLUDED.granted_by,
        granted_at = now(),
        expires_at = EXCLUDED.expires_at;

-- name: LockColumn :execrows
DELETE FROM column_unlock
WHERE database = $1 AND object_name = $2 AND column_name = $3 AND role = $4;

-- name: RecordColumnChange :exec
INSERT INTO column_change (
    id, actor_id, database, object_name, column_name, role, action, reason, expires_at
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9);

-- name: ListColumnChanges :many
SELECT c.database, c.object_name, c.column_name, c.role, c.action, c.reason,
       c.expires_at, c.changed_at,
       COALESCE(u.username, '')::text AS actor
FROM column_change c
LEFT JOIN app_user u ON u.id = c.actor_id
ORDER BY c.changed_at DESC
LIMIT $1::bigint;

-- name: ListDeniedTables :many
SELECT DISTINCT object_name
FROM table_denial
WHERE database = $1 AND role = ANY(@roles::text[])
ORDER BY object_name;

-- name: ListReachableDatabases :many
-- FEAT-058. The databases these roles reach, deduplicated.
SELECT DISTINCT database
FROM database_grant
WHERE role = ANY(@roles::text[])
ORDER BY database;

-- name: DatabaseIsReachable :one
SELECT EXISTS (
    SELECT 1 FROM database_grant
    WHERE database = $1 AND role = ANY(@roles::text[])
);

-- name: ListDatabaseGrants :many
SELECT id, role, database, reason, granted_at
FROM database_grant
ORDER BY database, role;

-- name: GrantDatabase :execrows
INSERT INTO database_grant (id, role, database, reason, granted_by)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (role, database) DO NOTHING;

-- name: RevokeDatabase :execrows
DELETE FROM database_grant WHERE role = $1 AND database = $2;

-- name: RecordDatabaseGrantChange :exec
INSERT INTO database_grant_change (id, actor_id, role, database, action, reason)
VALUES ($1, $2, $3, $4, $5, $6);

-- name: ListDatabaseGrantChanges :many
-- FEAT-058 AC-7. Who opened or closed a door onto a database. Append-only, and
-- read newest first because the question is almost always "what changed".
SELECT c.role, c.database, c.action, c.reason, c.changed_at,
       COALESCE(u.username, '')::text AS actor
FROM database_grant_change c
LEFT JOIN app_user u ON u.id = c.actor_id
ORDER BY c.changed_at DESC
-- Cast so the parameter is a bigint: an int32 here means the layers above have
-- to narrow a Go int to reach it, and a narrowing conversion is a thing to
-- prove safe on every path rather than a limit.
LIMIT $1::bigint;
