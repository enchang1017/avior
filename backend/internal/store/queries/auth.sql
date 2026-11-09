-- name: GetUserByUsername :one
-- FEAT-007 AC-6. Matched without regard to case, because the directory accepts
-- the name that way and an operator who types it differently is the same
-- operator. lower(username) carries the unique index, so this is a lookup on it
-- rather than a scan.
SELECT u.id, u.username, u.password_hash, u.totp_secret, u.disabled, u.declared, u.kind
FROM app_user u
WHERE lower(u.username) = lower($1);

-- name: ListRolesForUser :many
SELECT role FROM app_user_role WHERE user_id = $1 ORDER BY role;

-- name: CreateSession :one
INSERT INTO user_session (id, user_id, token_hash, expires_at)
VALUES ($1, $2, $3, $4)
RETURNING id, user_id, expires_at, created_at;

-- name: GetSessionByTokenHash :one
SELECT s.id, s.user_id, s.expires_at, u.username, u.disabled
FROM user_session s
JOIN app_user u ON u.id = s.user_id
WHERE s.token_hash = $1 AND s.expires_at > now();

-- name: DeleteSession :exec
DELETE FROM user_session WHERE id = $1;

-- name: DeleteExpiredSessions :exec
DELETE FROM user_session WHERE expires_at <= now();

-- name: ListOperators :many
-- Every operator with the roles they hold, gathered rather than joined: a
-- second row per role would make the caller assemble what the query can.
SELECT u.id, u.username, u.disabled, u.created_at, u.kind,
       COALESCE(
           ARRAY(SELECT r.role FROM app_user_role r WHERE r.user_id = u.id ORDER BY r.role),
           '{}'::text[]
       )::text[] AS roles
FROM app_user u
ORDER BY u.username;

-- name: FindOperator :one
SELECT u.id, u.username, u.disabled, u.created_at, u.kind,
       COALESCE(
           ARRAY(SELECT r.role FROM app_user_role r WHERE r.user_id = u.id ORDER BY r.role),
           '{}'::text[]
       )::text[] AS roles
FROM app_user u
WHERE u.id = $1;

-- name: GrantRole :execrows
-- DO NOTHING rather than an error: granting a role somebody already holds is
-- not a mistake, and a page that refused would make a double click a failure.
-- The row count says whether anything changed, which is what decides if the
-- change is recorded.
INSERT INTO app_user_role (user_id, role)
VALUES ($1, $2)
ON CONFLICT (user_id, role) DO NOTHING;

-- name: RevokeRole :execrows
DELETE FROM app_user_role WHERE user_id = $1 AND role = $2;

-- name: SetOperatorDisabled :execrows
-- The WHERE clause makes this idempotent the way the predecessor's read-then-compare
-- is, without the race between the read and the write.
UPDATE app_user SET disabled = $2 WHERE id = $1 AND disabled <> $2;

-- name: RecordAccountChange :exec
INSERT INTO account_change (id, actor_id, subject_id, action, role)
VALUES ($1, $2, $3, $4, $5);

-- name: ProvisionOperator :one
-- FEAT-007. The account the directory vouched for, made on first sign-in.
--
-- ON CONFLICT DO UPDATE rather than DO NOTHING: two first sign-ins can race,
-- and DO NOTHING returns no row to the loser, which would refuse an operator
-- the directory just authenticated. The update is a no-op that returns the row
-- that won.
--
-- No password hash and no TOTP secret. The directory replaces the password, and
-- a second factor is something an administrator enrols; a row carrying an empty
-- hash cannot be signed in to by the store verifier, which is the point.
--
-- `xmax = 0` distinguishes the row this statement inserted from one it merely
-- touched, which is what tells a first sign-in apart from a race with another.
-- Only the former grants a role and writes a record.
--
-- FEAT-056 AC-4. A name a deployment declared is not one the directory can
-- take. The guard is on the whole statement rather than on a prior read: two
-- sign-ins racing a declaration must not both get past a check that passed
-- before either wrote.
--
-- FEAT-063 AC-2. Nor is a service account's prefix: no person arrives as one.
-- The verifier refuses the name before the directory is asked, and this states
-- the same rule where the row is made.
INSERT INTO app_user (id, username, password_hash, totp_secret)
SELECT $1, $2, '', ''
WHERE NOT EXISTS (
    SELECT 1 FROM app_user WHERE declared AND lower(username) = lower($2)
)
  AND lower($2) NOT LIKE 'svc-%'
-- The update keeps the spelling already stored rather than taking EXCLUDED's.
-- The conflict is now case-insensitive, so EXCLUDED carries whatever case the
-- loser of the race happened to type, and writing it would rename the account
-- under whoever signed in most recently.
ON CONFLICT (lower(username)) DO UPDATE SET username = app_user.username
RETURNING id, username, password_hash, totp_secret, disabled, declared, (xmax = 0) AS inserted;

-- name: DeclareAccount :one
-- FEAT-056. The account a deployment's file names.
--
-- The password hash is rewritten every time, so a password changed in the file
-- is in force after a restart (AC-8). `disabled` is deliberately never touched:
-- startup does not unblock, or anybody who can restart a pod could undo a block
-- (AC-11).
INSERT INTO app_user (id, username, password_hash, totp_secret, declared)
VALUES ($1, $2, $3, '', true)
ON CONFLICT (lower(username)) DO UPDATE
    SET password_hash = EXCLUDED.password_hash, declared = true
RETURNING id, username, declared, disabled, (xmax = 0) AS inserted;

-- name: ListDeclaredAccounts :many
SELECT u.id, u.username, u.disabled,
       COALESCE(
           (SELECT array_agg(r.role ORDER BY r.role)
            FROM app_user_role r WHERE r.user_id = u.id),
           ARRAY[]::text[]
       )::text[] AS roles
FROM app_user u
WHERE u.declared
ORDER BY u.username;

-- name: UndeclareAccount :execrows
-- Dropped from the file: blocked, never deleted. The audit trail and the
-- account record both name it, and deleting would orphan them (AC-10).
UPDATE app_user
SET declared = false, disabled = true
WHERE id = $1 AND NOT disabled;

-- name: RecordProvision :exec
-- FEAT-059 AC-9. What was provisioned, by whom, and whether this call was the
-- one that created it.
INSERT INTO provision_change (id, actor_id, kind, name, created)
VALUES ($1, $2, $3, $4, $5);
