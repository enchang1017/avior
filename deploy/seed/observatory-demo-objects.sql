-- Run against the lineage demo database. Prints the store statements that give
-- each of its objects a history, as though the observatory had watched it for
-- sixty days: the counters as they stand now, so the first real snapshot sees
-- no reset, and a last use spread over ten weeks so some objects are flagged
-- and some are not. The shop_settle and settle_bonus families are placed by hand.
-- FEAT-073: each routine's time as it stands is the baseline the next real
-- snapshot continues from, and each routine used in the last thirty days has
-- timing history up to its last use, at a speed of its own.

WITH objects AS (
    SELECT 'table' AS kind, s.schemaname AS schema_name, s.relname AS name, '' AS signature,
           NULL::text AS parent, true AS tracked, NULL::text AS reason, false AS constraint_backed,
           s.n_tup_ins + s.n_tup_upd + s.n_tup_del AS counter,
           pg_total_relation_size(s.relid) AS size_bytes,
           NULL::float8 AS total_ms, NULL::float8 AS self_ms
    FROM pg_stat_user_tables s
    UNION ALL
    SELECT 'index', s.schemaname, s.indexrelname, '', s.relname, true, NULL,
           EXISTS (SELECT 1 FROM pg_constraint c WHERE c.conindid = s.indexrelid AND c.contype IN ('p', 'u', 'x')),
           s.idx_scan, NULL, NULL, NULL
    FROM pg_stat_user_indexes s
    UNION ALL
    SELECT CASE WHEN p.prokind = 'p' THEN 'procedure' ELSE 'function' END, n.nspname, p.proname,
           pg_get_function_identity_arguments(p.oid), NULL, l.lanname <> 'sql',
           CASE WHEN l.lanname = 'sql' THEN 'LANGUAGE sql needs track_functions = all' END, false,
           COALESCE(f.calls, 0), NULL,
           CASE WHEN l.lanname <> 'sql' THEN COALESCE(f.total_time, 0) END,
           CASE WHEN l.lanname <> 'sql' THEN COALESCE(f.self_time, 0) END
    FROM pg_proc p
    JOIN pg_namespace n ON n.oid = p.pronamespace
    JOIN pg_language l ON l.oid = p.prolang
    LEFT JOIN pg_stat_user_functions f ON f.funcid = p.oid
    WHERE p.prokind IN ('f', 'p') AND l.lanname NOT IN ('c', 'internal')
      AND n.nspname NOT LIKE 'pg\_%' AND n.nspname <> 'information_schema'
      AND NOT EXISTS (SELECT 1 FROM pg_depend d
                      WHERE d.classid = 'pg_proc'::regclass AND d.objid = p.oid AND d.deptype = 'e')
),
placed AS (
    SELECT o.*,
           CASE
               WHEN NOT o.tracked THEN NULL
               WHEN o.name = 'shop_settle_order_26_09_19' THEN 50
               WHEN o.name = 'shop_settle_order_26_09_21' THEN 35
               WHEN o.name = 'shop_settle_order_26_09_23' THEN NULL
               WHEN o.name = 'shop_settle_order_26_09_24' THEN 3
               WHEN o.name = 'shop_settle_order_26_09_25' THEN 40
               WHEN o.name = 'shop_settle_order_26_13_40' THEN 5
               WHEN o.name = 'settle_bonus_26_08_01' THEN 45
               WHEN o.name = 'settle_bonus_26_09_10' AND o.signature LIKE '%bigint%' THEN 2
               WHEN o.name = 'settle_bonus_26_09_10' THEN 1
               WHEN o.name = 'close_round_26_09_01' THEN 33
               WHEN o.kind IN ('function', 'procedure') AND o.name IN ('log_round', 'place_round', 'settle_round')
                   THEN 0
               WHEN o.kind IN ('function', 'procedure') AND o.name IN ('nightly_close', 'player_email') THEN 1
               WHEN o.kind IN ('function', 'procedure') AND o.name = 'top_players' THEN 2
               WHEN o.kind IN ('function', 'procedure') AND o.name = 'apply_adjustment' THEN 4
               WHEN abs(hashtext(o.schema_name || o.name || o.signature)) % 9 = 0 THEN NULL
               ELSE abs(hashtext(o.schema_name || o.name || o.signature)) % 70
           END AS days
    FROM objects o
)
SELECT format(
    'INSERT INTO observatory_object_usage (database, node, kind, schema_name, name, signature, parent, counter, '
    'last_increase_at, last_read_at, tracked, not_tracked_reason, constraint_backed, first_seen_at, last_seen_at, '
    'total_ms, self_ms) '
    'VALUES (%L, %L, %L, %L, %L, %L, %L, %s, %s, %s, %L, %L, %L, now() - interval ''60 days'', now() - interval ''1 hour'', '
    '%L, %L) ON CONFLICT DO NOTHING;',
    'aviorlineage', 'primary', kind, schema_name, name, signature, parent, counter,
    CASE WHEN days IS NULL OR kind = 'index' THEN 'NULL'
         WHEN kind = 'table' THEN format('now() - interval ''%s days''', days + 4)
         ELSE format('now() - interval ''%s days''', days) END,
    CASE WHEN days IS NULL OR kind NOT IN ('table', 'index') THEN 'NULL'
         ELSE format('now() - interval ''%s days''', days) END,
    tracked, reason, constraint_backed, total_ms, self_ms)
FROM placed
UNION ALL
SELECT format(
    'INSERT INTO observatory_routine_timing (database, node, kind, schema_name, name, signature, '
    'interval_start, interval_end, calls, total_ms, self_ms) '
    'SELECT %L, %L, %L, %L, %L, %L, e - interval ''15 minutes'', e, c, c * %s * m, c * %s * m * %s '
    'FROM (SELECT e, 1 + abs(hashtext(%L || e::text)) %% %s AS c, 0.6 + abs(hashtext(e::text || %L)) %% 80 / 100.0 AS m '
    '      FROM generate_series(date_trunc(''hour'', now()) - interval ''30 days'', '
    '                           date_trunc(''hour'', now()) - interval ''%s days'', interval ''15 minutes'') AS e) AS i '
    'WHERE %s ON CONFLICT DO NOTHING;',
    'aviorlineage', 'primary', kind, schema_name, name, signature,
    mean_ms, mean_ms, self_share, name, max_calls, name, greatest(days, 1),
    CASE WHEN daily THEN 'extract(hour FROM e) = 2 AND extract(minute FROM e) = 15'
         ELSE format('abs(hashtext(e::text || %L)) %% 100 < %s', schema_name || name || signature, activity) END)
FROM (
    SELECT p.*,
           CASE
               WHEN name LIKE 'shop_settle_order%' THEN 0.8
               WHEN name LIKE 'settle_bonus%' THEN 110
               WHEN name LIKE 'close_round%' THEN 2400
               WHEN name = 'nightly_close' THEN 6500
               WHEN name = 'archive_table' THEN 1800
               WHEN name = 'settle_round' THEN 95
               WHEN name = 'place_round' THEN 4
               WHEN name = 'top_players' THEN 45
               WHEN name = 'apply_adjustment' THEN 12
               WHEN name = 'player_email' THEN 0.3
               WHEN name = 'log_round' THEN 0.15
               ELSE 1 + abs(hashtext(name)) % 30
           END AS mean_ms,
           CASE
               WHEN name = 'nightly_close' THEN 0.15
               WHEN name LIKE 'close_round%' THEN 0.4
               WHEN name = 'settle_round' THEN 0.55
               WHEN name = 'place_round' THEN 0.6
               WHEN name LIKE 'settle_bonus%' THEN 0.7
               WHEN name = 'apply_adjustment' THEN 0.8
               WHEN name = 'archive_table' THEN 0.9
               ELSE 1
           END AS self_share,
           CASE
               WHEN name LIKE 'close_round%' OR name IN ('nightly_close', 'archive_table') THEN 1
               WHEN name LIKE 'shop_settle_order%' OR name = 'log_round' THEN 180
               WHEN name IN ('place_round', 'player_email') THEN 60
               ELSE 8
           END AS max_calls,
           CASE WHEN name LIKE 'shop_settle_order%' OR name = 'log_round' THEN 60
                WHEN name LIKE 'settle_bonus%' THEN 30
                ELSE 25 END AS activity,
           name LIKE 'close_round%' OR name IN ('nightly_close', 'archive_table') AS daily
    FROM placed p
    WHERE p.kind IN ('function', 'procedure') AND p.tracked AND p.days IS NOT NULL AND p.days < 30
) AS timed
UNION ALL
SELECT format(
    'INSERT INTO observatory_table_size_daily (database, schema_name, name, day, size_bytes) '
    'SELECT %L, %L, %L, (now() AT TIME ZONE ''UTC'')::date - d, (%s * (1 - d / 100.0))::bigint '
    'FROM generate_series(0, 59) AS d ON CONFLICT DO NOTHING;',
    'aviorlineage', schema_name, name, size_bytes)
FROM objects
WHERE kind = 'table';
