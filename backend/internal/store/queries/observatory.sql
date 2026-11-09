-- FEAT-071 phases 2 and 3. The snapshot job's history.

-- name: GetObservatoryDatabaseState :one
SELECT /* avior:observatory */ database, node, first_snapshot_at, routines_tracked_since, stats_reset,
       postmaster_start, last_reset_at, last_snapshot_at
FROM observatory_database_state
WHERE database = $1 AND node = $2;

-- name: ListObservatoryDatabaseStates :many
SELECT /* avior:observatory */ database, node, first_snapshot_at, routines_tracked_since, stats_reset,
       postmaster_start, last_reset_at, last_snapshot_at
FROM observatory_database_state
WHERE database = ANY(sqlc.arg('databases')::text[]) AND node = sqlc.arg('node')
ORDER BY database;

-- name: UpsertObservatoryDatabaseState :exec
INSERT /* avior:observatory */ INTO observatory_database_state (
    database, node, first_snapshot_at, routines_tracked_since, stats_reset,
    postmaster_start, last_reset_at, last_snapshot_at
) VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
ON CONFLICT (database, node) DO UPDATE SET
    routines_tracked_since = EXCLUDED.routines_tracked_since,
    stats_reset            = EXCLUDED.stats_reset,
    postmaster_start       = EXCLUDED.postmaster_start,
    last_reset_at          = EXCLUDED.last_reset_at,
    last_snapshot_at       = EXCLUDED.last_snapshot_at;

-- name: ListObservatoryUsage :many
SELECT /* avior:observatory */ database, kind, schema_name, name, signature, parent, counter,
       last_increase_at, last_read_at, tracked, not_tracked_reason,
       constraint_backed, first_seen_at, last_seen_at, disappeared_at, total_ms, self_ms
FROM observatory_object_usage
WHERE database = ANY(sqlc.arg('databases')::text[]) AND node = sqlc.arg('node')
  AND (sqlc.arg('include_gone')::boolean OR disappeared_at IS NULL)
ORDER BY database, kind, schema_name, name, signature;

-- name: UpsertObservatoryUsage :exec
INSERT /* avior:observatory */ INTO observatory_object_usage (
    database, node, kind, schema_name, name, signature, parent, counter,
    last_increase_at, last_read_at, tracked, not_tracked_reason,
    constraint_backed, first_seen_at, last_seen_at, disappeared_at, total_ms, self_ms
)
SELECT sqlc.arg('database')::text, sqlc.arg('node')::text, u.kind, u.schema_name, u.name, u.signature,
       NULLIF(u.parent, ''), u.counter, u.last_increase_at, u.last_read_at, u.tracked,
       NULLIF(u.not_tracked_reason, ''), u.constraint_backed, u.first_seen_at,
       sqlc.arg('seen_at')::timestamptz, NULL,
       CASE WHEN u.timed THEN u.total_ms END, CASE WHEN u.timed THEN u.self_ms END
FROM (SELECT
    unnest(sqlc.arg('kinds')::text[]) AS kind,
    unnest(sqlc.arg('schemas')::text[]) AS schema_name,
    unnest(sqlc.arg('names')::text[]) AS name,
    unnest(sqlc.arg('signatures')::text[]) AS signature,
    unnest(sqlc.arg('parents')::text[]) AS parent,
    unnest(sqlc.arg('counters')::bigint[]) AS counter,
    unnest(sqlc.arg('last_increases')::timestamptz[]) AS last_increase_at,
    unnest(sqlc.arg('last_reads')::timestamptz[]) AS last_read_at,
    unnest(sqlc.arg('tracked')::boolean[]) AS tracked,
    unnest(sqlc.arg('reasons')::text[]) AS not_tracked_reason,
    unnest(sqlc.arg('constraint_backed')::boolean[]) AS constraint_backed,
    unnest(sqlc.arg('first_seen')::timestamptz[]) AS first_seen_at,
    unnest(sqlc.arg('timed')::boolean[]) AS timed,
    unnest(sqlc.arg('total_ms')::float8[]) AS total_ms,
    unnest(sqlc.arg('self_ms')::float8[]) AS self_ms
) AS u
ON CONFLICT (database, node, kind, schema_name, name, signature) DO UPDATE SET
    parent             = EXCLUDED.parent,
    counter            = EXCLUDED.counter,
    last_increase_at   = EXCLUDED.last_increase_at,
    last_read_at       = EXCLUDED.last_read_at,
    tracked            = EXCLUDED.tracked,
    not_tracked_reason = EXCLUDED.not_tracked_reason,
    constraint_backed  = EXCLUDED.constraint_backed,
    last_seen_at       = EXCLUDED.last_seen_at,
    disappeared_at     = NULL,
    total_ms           = EXCLUDED.total_ms,
    self_ms            = EXCLUDED.self_ms;

-- name: MarkObservatoryUsageGone :exec
UPDATE /* avior:observatory */ observatory_object_usage
SET disappeared_at = sqlc.arg('seen_at')::timestamptz
WHERE database = sqlc.arg('database') AND node = sqlc.arg('node')
  AND disappeared_at IS NULL
  AND last_seen_at < sqlc.arg('seen_at')::timestamptz;

-- name: AddObservatoryUsageDaily :exec
INSERT /* avior:observatory */ INTO observatory_usage_daily (database, node, kind, schema_name, name, signature, day, delta)
SELECT sqlc.arg('database')::text, sqlc.arg('node')::text, d.kind, d.schema_name, d.name, d.signature,
       sqlc.arg('day')::date, d.delta
FROM (SELECT
    unnest(sqlc.arg('kinds')::text[]) AS kind,
    unnest(sqlc.arg('schemas')::text[]) AS schema_name,
    unnest(sqlc.arg('names')::text[]) AS name,
    unnest(sqlc.arg('signatures')::text[]) AS signature,
    unnest(sqlc.arg('deltas')::bigint[]) AS delta
) AS d
ON CONFLICT (database, node, kind, schema_name, name, signature, day)
DO UPDATE SET delta = observatory_usage_daily.delta + EXCLUDED.delta;

-- name: AddObservatoryRoutineTiming :exec
-- FEAT-073. One interval's timing per routine whose calls rose.
INSERT /* avior:observatory */ INTO observatory_routine_timing (
    database, node, kind, schema_name, name, signature, interval_start, interval_end, calls, total_ms, self_ms
)
SELECT sqlc.arg('database')::text, sqlc.arg('node')::text, t.kind, t.schema_name, t.name, t.signature,
       sqlc.arg('interval_start')::timestamptz, sqlc.arg('interval_end')::timestamptz, t.calls, t.total_ms, t.self_ms
FROM (SELECT
    unnest(sqlc.arg('kinds')::text[]) AS kind,
    unnest(sqlc.arg('schemas')::text[]) AS schema_name,
    unnest(sqlc.arg('names')::text[]) AS name,
    unnest(sqlc.arg('signatures')::text[]) AS signature,
    unnest(sqlc.arg('calls')::bigint[]) AS calls,
    unnest(sqlc.arg('total_ms')::float8[]) AS total_ms,
    unnest(sqlc.arg('self_ms')::float8[]) AS self_ms
) AS t
ON CONFLICT (database, node, kind, schema_name, name, signature, interval_end) DO NOTHING;

-- name: SumObservatoryRoutineTiming :many
-- FEAT-073. Each routine's calls and time over the intervals that ended in
-- the window.
SELECT /* avior:observatory */ database, kind, schema_name, name, signature,
       sum(calls)::bigint AS calls, sum(total_ms)::float8 AS total_ms, sum(self_ms)::float8 AS self_ms
FROM observatory_routine_timing
WHERE database = ANY(sqlc.arg('databases')::text[]) AND node = sqlc.arg('node')
  AND interval_end > sqlc.arg('from_at')::timestamptz AND interval_end <= sqlc.arg('to_at')::timestamptz
GROUP BY database, kind, schema_name, name, signature;

-- name: UpsertObservatoryDatabaseSize :exec
INSERT /* avior:observatory */ INTO observatory_db_size_daily (database, day, size_bytes)
VALUES ($1, $2, $3)
ON CONFLICT (database, day) DO UPDATE SET size_bytes = EXCLUDED.size_bytes;

-- name: UpsertObservatoryTableSizes :exec
INSERT /* avior:observatory */ INTO observatory_table_size_daily (database, schema_name, name, day, size_bytes)
SELECT sqlc.arg('database')::text, t.schema_name, t.name, sqlc.arg('day')::date, t.size_bytes
FROM (SELECT
    unnest(sqlc.arg('schemas')::text[]) AS schema_name,
    unnest(sqlc.arg('names')::text[]) AS name,
    unnest(sqlc.arg('sizes')::bigint[]) AS size_bytes
) AS t
ON CONFLICT (database, schema_name, name, day) DO UPDATE SET size_bytes = EXCLUDED.size_bytes;

-- name: RecordObservatoryReset :exec
INSERT /* avior:observatory */ INTO observatory_reset (database, node, detected_at, reason) VALUES ($1, $2, $3, $4);

-- name: RecordObservatoryRun :exec
INSERT /* avior:observatory */ INTO observatory_run (started_at, finished_at, databases_read, databases_failed, flags_withheld)
VALUES ($1, $2, sqlc.arg('databases_read')::text[], sqlc.arg('databases_failed')::text[], $3);

-- name: GetLatestObservatoryRun :one
SELECT /* avior:observatory */ started_at, finished_at, databases_read, databases_failed, flags_withheld
FROM observatory_run
ORDER BY finished_at DESC
LIMIT 1;

-- name: ListObservatoryDatabaseSizes :many
SELECT /* avior:observatory */ database, day, size_bytes
FROM observatory_db_size_daily
WHERE database = ANY(sqlc.arg('databases')::text[]) AND day >= sqlc.arg('since')::date
ORDER BY database, day;

-- name: ListObservatoryTableSizes :many
-- Each table's size on the database's newest day, and on the newest day at or
-- before the comparison day.
SELECT /* avior:observatory */ t.schema_name, t.name, t.size_bytes, COALESCE(b.size_bytes, 0)::bigint AS size_before,
       (b.size_bytes IS NOT NULL)::boolean AS has_before
FROM observatory_table_size_daily t
LEFT JOIN LATERAL (
    SELECT o.size_bytes FROM observatory_table_size_daily o
    WHERE o.database = t.database AND o.schema_name = t.schema_name AND o.name = t.name
      AND o.day <= sqlc.arg('before')::date
    ORDER BY o.day DESC LIMIT 1
) b ON true
WHERE t.database = sqlc.arg('database')::text
  AND t.day = (SELECT max(m.day) FROM observatory_table_size_daily m WHERE m.database = sqlc.arg('database')::text);

-- name: RecordObservatoryStatements :exec
-- One hour's costliest statements, each with how much it cost since the last
-- row kept for it. A counter lower than the one kept was reset, and all of it
-- is new.
INSERT /* avior:observatory */ INTO observatory_statement_hourly (
    hour, query_id, database, role, calls, total_ms, rows, calls_delta, total_ms_delta, text
)
SELECT sqlc.arg('hour')::timestamptz, s.query_id, s.database, s.role, s.calls, s.total_ms, s.rows,
       CASE WHEN p.calls IS NULL OR p.calls > s.calls THEN s.calls ELSE s.calls - p.calls END,
       CASE WHEN p.total_ms IS NULL OR p.total_ms > s.total_ms THEN s.total_ms ELSE s.total_ms - p.total_ms END,
       s.text
FROM (SELECT
    unnest(sqlc.arg('query_ids')::text[]) AS query_id,
    unnest(sqlc.arg('databases')::text[]) AS database,
    unnest(sqlc.arg('roles')::text[]) AS role,
    unnest(sqlc.arg('calls')::bigint[]) AS calls,
    unnest(sqlc.arg('total_ms')::float8[]) AS total_ms,
    unnest(sqlc.arg('rows')::bigint[]) AS rows,
    unnest(sqlc.arg('texts')::text[]) AS text
) AS s
LEFT JOIN LATERAL (
    SELECT h.calls, h.total_ms FROM observatory_statement_hourly h
    WHERE h.query_id = s.query_id AND h.database = s.database AND h.role = s.role
      AND h.hour < sqlc.arg('hour')::timestamptz
    ORDER BY h.hour DESC LIMIT 1
) p ON true
ON CONFLICT (hour, query_id, database, role) DO UPDATE SET
    calls = EXCLUDED.calls, total_ms = EXCLUDED.total_ms, rows = EXCLUDED.rows,
    calls_delta = EXCLUDED.calls_delta, total_ms_delta = EXCLUDED.total_ms_delta, text = EXCLUDED.text;

-- name: RecordObservatoryTextView :exec
INSERT /* avior:observatory */ INTO observatory_text_view (user_id, database_filter, tab, texts) VALUES ($1, $2, $3, $4);

-- The purge, in batches of 10,000 rows so no run holds a long lock.

-- name: PurgeObservatoryDatabaseSizes :execrows
DELETE /* avior:observatory */ FROM observatory_db_size_daily
WHERE (database, day) IN (
    SELECT p.database, p.day FROM observatory_db_size_daily p WHERE p.day < sqlc.arg('before')::date LIMIT 10000
);

-- name: PurgeObservatoryTableSizes :execrows
DELETE /* avior:observatory */ FROM observatory_table_size_daily
WHERE (database, schema_name, name, day) IN (
    SELECT p.database, p.schema_name, p.name, p.day FROM observatory_table_size_daily p
    WHERE p.day < sqlc.arg('before')::date LIMIT 10000
);

-- name: PurgeObservatoryUsageDaily :execrows
DELETE /* avior:observatory */ FROM observatory_usage_daily
WHERE (database, node, kind, schema_name, name, signature, day) IN (
    SELECT p.database, p.node, p.kind, p.schema_name, p.name, p.signature, p.day
    FROM observatory_usage_daily p WHERE p.day < sqlc.arg('before')::date LIMIT 10000
);

-- name: PurgeObservatoryGoneUsage :execrows
DELETE /* avior:observatory */ FROM observatory_object_usage
WHERE (database, node, kind, schema_name, name, signature) IN (
    SELECT p.database, p.node, p.kind, p.schema_name, p.name, p.signature
    FROM observatory_object_usage p
    WHERE p.disappeared_at < sqlc.arg('before')::timestamptz LIMIT 10000
);

-- name: PurgeObservatoryStatements :execrows
DELETE /* avior:observatory */ FROM observatory_statement_hourly
WHERE (hour, query_id, database, role) IN (
    SELECT p.hour, p.query_id, p.database, p.role FROM observatory_statement_hourly p
    WHERE p.hour < sqlc.arg('before')::timestamptz LIMIT 10000
);

-- name: PurgeObservatoryRoutineTiming :execrows
DELETE /* avior:observatory */ FROM observatory_routine_timing
WHERE (database, node, kind, schema_name, name, signature, interval_end) IN (
    SELECT p.database, p.node, p.kind, p.schema_name, p.name, p.signature, p.interval_end
    FROM observatory_routine_timing p WHERE p.interval_end < sqlc.arg('before')::timestamptz LIMIT 10000
);

-- name: PurgeObservatoryRuns :execrows
DELETE /* avior:observatory */ FROM observatory_run
WHERE id IN (SELECT p.id FROM observatory_run p WHERE p.finished_at < sqlc.arg('before')::timestamptz LIMIT 10000);

-- name: PurgeObservatoryResets :execrows
DELETE /* avior:observatory */ FROM observatory_reset
WHERE id IN (SELECT p.id FROM observatory_reset p WHERE p.detected_at < sqlc.arg('before')::timestamptz LIMIT 10000);

-- name: ListObservatoryLargestTables :many
-- FEAT-072. The largest tables on each database's newest day, across the
-- databases, with each one's size on the newest day at or before the
-- comparison day.
WITH /* avior:observatory */ newest AS (
    SELECT m.database, max(m.day) AS day
    FROM observatory_table_size_daily m
    WHERE m.database = ANY(sqlc.arg('databases')::text[])
    GROUP BY m.database
), largest AS (
    SELECT t.database, t.schema_name, t.name, t.size_bytes
    FROM observatory_table_size_daily t
    JOIN newest n ON n.database = t.database AND n.day = t.day
    ORDER BY t.size_bytes DESC, t.database, t.schema_name, t.name
    LIMIT sqlc.arg('limit_count')::integer
)
SELECT l.database, l.schema_name, l.name, l.size_bytes,
       COALESCE(b.size_bytes, 0)::bigint AS size_before,
       (b.size_bytes IS NOT NULL)::boolean AS has_before
FROM largest l
LEFT JOIN LATERAL (
    SELECT o.size_bytes FROM observatory_table_size_daily o
    WHERE o.database = l.database AND o.schema_name = l.schema_name AND o.name = l.name
      AND o.day <= sqlc.arg('before')::date
    ORDER BY o.day DESC LIMIT 1
) b ON true
ORDER BY l.size_bytes DESC, l.database, l.schema_name, l.name;
