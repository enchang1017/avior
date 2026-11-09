-- name: SummariseAuditUsage :many
-- FEAT-071 phase 1. Elapsed and rows are over executed statements only: an
-- allowed row with an elapsed time. A refusal is counted by each distinct rule
-- it carries, once per statement.
WITH /* avior:observatory */ totals AS (
    SELECT a.user_id,
           a.database,
           count(*) AS statements,
           count(*) FILTER (WHERE a.decision = 'allow') AS allowed,
           count(*) FILTER (WHERE a.decision = 'deny') AS refused
    FROM audit_entry a
    WHERE a.created_at >= sqlc.arg('from_time')::timestamptz
      AND a.created_at < sqlc.arg('to_time')::timestamptz
      AND a.database = ANY (sqlc.arg('databases')::text[])
      AND (sqlc.narg('user_id')::uuid IS NULL OR a.user_id = sqlc.narg('user_id')::uuid)
    GROUP BY a.user_id, a.database
), timings AS (
    SELECT a.user_id,
           a.database,
           count(*) AS executed,
           percentile_disc(0.5) WITHIN GROUP (ORDER BY a.elapsed_ms)::integer AS p50,
           percentile_disc(0.95) WITHIN GROUP (ORDER BY a.elapsed_ms)::integer AS p95,
           coalesce(sum(a.row_count), 0)::bigint AS rows_total
    FROM audit_entry a
    WHERE a.decision = 'allow'
      AND a.elapsed_ms IS NOT NULL
      AND a.created_at >= sqlc.arg('from_time')::timestamptz
      AND a.created_at < sqlc.arg('to_time')::timestamptz
      AND a.database = ANY (sqlc.arg('databases')::text[])
      AND (sqlc.narg('user_id')::uuid IS NULL OR a.user_id = sqlc.narg('user_id')::uuid)
    GROUP BY a.user_id, a.database
), refusals AS (
    SELECT a.user_id, a.database, r.rule, count(*) AS n
    FROM audit_entry a
    CROSS JOIN LATERAL (
        SELECT DISTINCT coalesce(e ->> 'Rule', '') AS rule
        FROM jsonb_array_elements(a.reasons) e
    ) r
    WHERE a.decision = 'deny'
      AND a.created_at >= sqlc.arg('from_time')::timestamptz
      AND a.created_at < sqlc.arg('to_time')::timestamptz
      AND a.database = ANY (sqlc.arg('databases')::text[])
      AND (sqlc.narg('user_id')::uuid IS NULL OR a.user_id = sqlc.narg('user_id')::uuid)
    GROUP BY a.user_id, a.database, r.rule
)
SELECT t.user_id::uuid AS user_id,
       u.username::text AS username,
       t.database::text AS database,
       t.statements::bigint AS statements,
       t.allowed::bigint AS allowed,
       t.refused::bigint AS refused,
       coalesce(m.executed, 0)::bigint AS executed,
       m.p50 AS elapsed_p50_ms,
       m.p95 AS elapsed_p95_ms,
       coalesce(m.rows_total, 0)::bigint AS rows_total,
       coalesce((
           SELECT jsonb_agg(jsonb_build_object('reason', f.rule, 'count', f.n) ORDER BY f.n DESC, f.rule)
           FROM refusals f
           WHERE f.user_id = t.user_id AND f.database = t.database
       ), '[]'::jsonb)::jsonb AS refusals
FROM totals t
JOIN app_user u ON u.id = t.user_id
LEFT JOIN timings m ON m.user_id = t.user_id AND m.database = t.database
ORDER BY t.statements DESC, u.username, t.database;

-- name: UsageSeries :many
-- FEAT-072. Every bucket of the window, aligned to the UTC epoch, with the
-- ones nothing landed in answered as zeros.
WITH /* avior:observatory */ buckets AS (
    SELECT g AS start
    FROM generate_series(
        to_timestamp(floor(extract(epoch FROM sqlc.arg('from_time')::timestamptz) / sqlc.arg('bucket_seconds')::integer) * sqlc.arg('bucket_seconds')::integer),
        sqlc.arg('to_time')::timestamptz - interval '1 microsecond',
        make_interval(secs => sqlc.arg('bucket_seconds')::integer)
    ) g
), counted AS (
    SELECT to_timestamp(floor(extract(epoch FROM a.created_at) / sqlc.arg('bucket_seconds')::integer) * sqlc.arg('bucket_seconds')::integer) AS start,
           count(*) FILTER (WHERE a.decision = 'allow') AS allowed,
           count(*) FILTER (WHERE a.decision = 'deny') AS refused,
           (percentile_disc(0.5) WITHIN GROUP (ORDER BY a.elapsed_ms)
               FILTER (WHERE a.decision = 'allow' AND a.elapsed_ms IS NOT NULL))::integer AS p50,
           (percentile_disc(0.95) WITHIN GROUP (ORDER BY a.elapsed_ms)
               FILTER (WHERE a.decision = 'allow' AND a.elapsed_ms IS NOT NULL))::integer AS p95
    FROM audit_entry a
    WHERE a.created_at >= sqlc.arg('from_time')::timestamptz
      AND a.created_at < sqlc.arg('to_time')::timestamptz
      AND a.database = ANY (sqlc.arg('databases')::text[])
      AND (sqlc.narg('user_id')::uuid IS NULL OR a.user_id = sqlc.narg('user_id')::uuid)
    GROUP BY 1
)
SELECT b.start::timestamptz AS bucket_start,
       coalesce(c.allowed, 0)::bigint AS allowed,
       coalesce(c.refused, 0)::bigint AS refused,
       c.p50 AS elapsed_p50_ms,
       c.p95 AS elapsed_p95_ms
FROM buckets b
LEFT JOIN counted c ON c.start = b.start
ORDER BY b.start;

-- name: UsageTotals :one
-- FEAT-072. One window in all, elapsed over executed statements only. The
-- aggregate is joined onto one row so the elapsed figures read as nullable.
WITH /* avior:observatory */ totals AS (
    SELECT count(*) AS statements,
           count(*) FILTER (WHERE a.decision = 'allow') AS allowed,
           count(*) FILTER (WHERE a.decision = 'deny') AS refused,
           count(*) FILTER (WHERE a.decision = 'allow' AND a.elapsed_ms IS NOT NULL) AS executed,
           (percentile_disc(0.5) WITHIN GROUP (ORDER BY a.elapsed_ms)
               FILTER (WHERE a.decision = 'allow' AND a.elapsed_ms IS NOT NULL))::integer AS p50,
           (percentile_disc(0.95) WITHIN GROUP (ORDER BY a.elapsed_ms)
               FILTER (WHERE a.decision = 'allow' AND a.elapsed_ms IS NOT NULL))::integer AS p95
    FROM audit_entry a
    WHERE a.created_at >= sqlc.arg('from_time')::timestamptz
      AND a.created_at < sqlc.arg('to_time')::timestamptz
      AND a.database = ANY (sqlc.arg('databases')::text[])
      AND (sqlc.narg('user_id')::uuid IS NULL OR a.user_id = sqlc.narg('user_id')::uuid)
)
SELECT coalesce(t.statements, 0)::bigint AS statements,
       coalesce(t.allowed, 0)::bigint AS allowed,
       coalesce(t.refused, 0)::bigint AS refused,
       coalesce(t.executed, 0)::bigint AS executed,
       t.p50 AS elapsed_p50_ms,
       t.p95 AS elapsed_p95_ms
FROM (SELECT 1) AS one
LEFT JOIN totals t ON true;
