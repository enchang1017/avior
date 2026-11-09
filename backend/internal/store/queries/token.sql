-- FEAT-053. API tokens: a hash per row, and never the value.

-- name: CreateAPIToken :one
INSERT INTO api_token (id, user_id, name, token_hash, expires_at)
VALUES ($1, $2, $3, $4, $5)
RETURNING id, name, created_at, expires_at;

-- name: FindAPIToken :one
-- Compared on the hash. The account is joined here rather than resolved from
-- a session, because there is no session: what the account holds now is what
-- the token holds now, which is what lets disabling the account stop the token.
SELECT t.id, t.user_id, t.expires_at, t.revoked_at, u.username, u.disabled
FROM api_token t
JOIN app_user u ON u.id = t.user_id
WHERE t.token_hash = $1;

-- name: TouchAPIToken :exec
-- Once a minute at most. Every authenticated call would otherwise be a write
-- against the store, and "last used within the minute" is as precise as anybody
-- reading the column needs.
UPDATE api_token
SET last_used_at = now()
WHERE id = $1
  AND (last_used_at IS NULL OR last_used_at < now() - interval '1 minute');

-- name: ListAPITokensForUser :many
SELECT id, name, created_at, expires_at, last_used_at, revoked_at
FROM api_token
WHERE user_id = $1
ORDER BY created_at DESC;

-- name: RevokeAPIToken :execrows
-- Scoped to the owner in the predicate rather than checked first: a token that
-- is not theirs revokes nothing and the row count says so, which the handler
-- reports as not found rather than as forbidden.
UPDATE api_token
SET revoked_at = now()
WHERE id = $1 AND user_id = $2 AND revoked_at IS NULL;
