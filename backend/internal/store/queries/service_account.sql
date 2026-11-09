-- FEAT-063. Accounts for agents that are nobody's person.

-- name: CreateServiceAccount :one
-- DO NOTHING on the name, compared without regard to case: a name already
-- taken by anybody, person or service, returns no row, which the adapter
-- reports as taken rather than as a fault.
INSERT INTO app_user (id, username, password_hash, totp_secret, kind, description, created_by)
VALUES ($1, $2, '', '', 'service', $3, $4)
ON CONFLICT (lower(username)) DO NOTHING
RETURNING id, username, description, disabled, created_at;

-- name: ListServiceAccounts :many
SELECT u.id, u.username, u.description, u.disabled, u.created_at,
       COALESCE(c.username, '')::text AS created_by,
       COALESCE(
           ARRAY(SELECT r.role FROM app_user_role r WHERE r.user_id = u.id ORDER BY r.role),
           '{}'::text[]
       )::text[] AS roles
FROM app_user u
LEFT JOIN app_user c ON c.id = u.created_by
WHERE u.kind = 'service'
ORDER BY u.username;

-- name: ListServiceAccountTokens :many
SELECT t.id, t.user_id, t.name, t.created_at, t.expires_at, t.last_used_at, t.revoked_at
FROM api_token t
JOIN app_user u ON u.id = t.user_id
WHERE u.kind = 'service'
ORDER BY t.created_at DESC;

-- name: LockServiceAccount :one
-- Held for the rest of the mint, so two mints racing for the last free slot
-- are serialised on the account rather than both counting one live token and
-- both inserting.
SELECT id FROM app_user WHERE id = $1 AND kind = 'service' FOR UPDATE;

-- name: ListLiveTokenNames :many
SELECT name
FROM api_token
WHERE user_id = $1 AND revoked_at IS NULL AND expires_at > now()
ORDER BY created_at;

-- name: RevokeServiceAccountToken :execrows
UPDATE api_token t
SET revoked_at = now()
FROM app_user u
WHERE t.id = $1
  AND t.user_id = $2
  AND u.id = t.user_id
  AND u.kind = 'service'
  AND t.revoked_at IS NULL;
