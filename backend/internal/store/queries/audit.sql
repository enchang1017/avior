-- name: RecordAuditEntry :one
INSERT INTO audit_entry (id, user_id, instance_id, database, statement, decision, reasons,
                         token_id, requested_by)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
RETURNING id, created_at;

-- name: CompleteAuditEntry :exec
UPDATE audit_entry SET row_count = $2, elapsed_ms = $3 WHERE id = $1;

-- name: ListAuditEntries :many
SELECT a.id, u.username, a.instance_id, a.database, a.statement, a.decision,
       a.reasons, a.row_count, a.elapsed_ms, a.created_at
FROM audit_entry a
JOIN app_user u ON u.id = a.user_id
WHERE (sqlc.narg('user_id')::uuid IS NULL OR a.user_id = sqlc.narg('user_id')::uuid)
  AND (sqlc.narg('operator')::text IS NULL OR u.username ILIKE '%' || sqlc.narg('operator')::text || '%')
  AND (sqlc.narg('statement')::text IS NULL OR a.statement ILIKE '%' || sqlc.narg('statement')::text || '%')
  AND (sqlc.narg('decision')::text IS NULL OR a.decision = sqlc.narg('decision')::text)
  AND (sqlc.narg('from_time')::timestamptz IS NULL OR a.created_at >= sqlc.narg('from_time')::timestamptz)
  AND (sqlc.narg('to_time')::timestamptz IS NULL OR a.created_at <= sqlc.narg('to_time')::timestamptz)
ORDER BY a.created_at DESC, a.id DESC
LIMIT sqlc.arg('row_limit') OFFSET sqlc.arg('row_offset');

-- name: CountAuditEntriesByHour :many
WITH hours AS (
    SELECT generate_series(
        sqlc.arg('first_hour')::timestamptz,
        sqlc.arg('last_hour')::timestamptz,
        interval '1 hour') AS start
), counted AS (
    SELECT date_trunc('hour', a.created_at, 'UTC') AS start,
           count(*) FILTER (WHERE a.decision = 'allow') AS allowed,
           count(*) FILTER (WHERE a.decision = 'deny') AS denied
    FROM audit_entry a
    JOIN app_user u ON u.id = a.user_id
    WHERE (sqlc.narg('user_id')::uuid IS NULL OR a.user_id = sqlc.narg('user_id')::uuid)
      AND (sqlc.narg('operator')::text IS NULL OR u.username ILIKE '%' || sqlc.narg('operator')::text || '%')
      AND (sqlc.narg('statement')::text IS NULL OR a.statement ILIKE '%' || sqlc.narg('statement')::text || '%')
      AND (sqlc.narg('decision')::text IS NULL OR a.decision = sqlc.narg('decision')::text)
      AND a.created_at >= sqlc.arg('from_time')::timestamptz
      AND a.created_at < sqlc.arg('to_time')::timestamptz
    GROUP BY 1
)
SELECT h.start::timestamptz AS start,
       coalesce(c.allowed, 0)::bigint AS allowed,
       coalesce(c.denied, 0)::bigint AS denied
FROM hours h
LEFT JOIN counted c ON c.start = h.start
ORDER BY h.start;
