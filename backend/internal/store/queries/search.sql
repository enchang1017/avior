-- FEAT-066. Each kind is one ILIKE with a LIMIT, on tables small enough that no
-- index is warranted. The rough order below keeps the best candidates inside
-- the limit; the exact ranking is platform/search's, applied afterwards.

-- name: SearchWriteRequests :many
SELECT id, database, target_table, reason, state
FROM write_request
WHERE database = ANY(sqlc.arg('databases')::text[])
  AND (id::text ILIKE '%' || sqlc.arg('pattern')::text || '%'
       OR target_table ILIKE '%' || sqlc.arg('pattern')::text || '%'
       OR reason ILIKE '%' || sqlc.arg('pattern')::text || '%')
ORDER BY LEAST(
           CASE WHEN id::text ILIKE sqlc.arg('pattern')::text || '%' THEN 0 ELSE 1 END,
           CASE WHEN target_table ILIKE sqlc.arg('pattern')::text || '%' THEN 0 ELSE 1 END,
           CASE WHEN reason ILIKE sqlc.arg('pattern')::text || '%' THEN 0 ELSE 1 END),
         requested_at DESC
LIMIT sqlc.arg('row_limit');

-- name: SearchReleases :many
SELECT id, database, state
FROM release
WHERE database = ANY(sqlc.arg('databases')::text[])
  AND (id::text ILIKE '%' || sqlc.arg('pattern')::text || '%'
       OR database ILIKE '%' || sqlc.arg('pattern')::text || '%')
ORDER BY LEAST(
           CASE WHEN id::text ILIKE sqlc.arg('pattern')::text || '%' THEN 0 ELSE 1 END,
           CASE WHEN database ILIKE sqlc.arg('pattern')::text || '%' THEN 0 ELSE 1 END),
         submitted_at DESC
LIMIT sqlc.arg('row_limit');

-- name: SearchAccounts :many
SELECT id, username, kind
FROM app_user
WHERE username ILIKE '%' || sqlc.arg('pattern')::text || '%'
ORDER BY CASE WHEN username ILIKE sqlc.arg('pattern')::text || '%' THEN 0 ELSE 1 END,
         length(username), username
LIMIT sqlc.arg('row_limit');
