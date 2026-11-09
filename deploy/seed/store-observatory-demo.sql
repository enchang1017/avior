-- FEAT-071's demo history in the store, local development only: sixty days of
-- database sizes, tracking that began sixty days ago, a run a day old so the
-- backend's first tick takes a real snapshot, and thirty days of submissions
-- by seven operators over three databases for the usage tab and the overview.

-- The seed has just rebuilt these databases, so what the store kept about the
-- previous ones would read as a reset.
DELETE FROM observatory_object_usage WHERE database IN ('aviorlineage', 'aviorfixture', 'aviordeploy');
DELETE FROM observatory_database_state WHERE database IN ('aviorlineage', 'aviorfixture', 'aviordeploy');
DELETE FROM observatory_db_size_daily WHERE database IN ('aviorlineage', 'aviorfixture', 'aviordeploy');
DELETE FROM observatory_table_size_daily WHERE database IN ('aviorlineage', 'aviorfixture', 'aviordeploy');
DELETE FROM observatory_routine_timing WHERE database IN ('aviorlineage', 'aviorfixture', 'aviordeploy');

INSERT INTO observatory_database_state (
    database, node, first_snapshot_at, routines_tracked_since, stats_reset, postmaster_start, last_snapshot_at
)
SELECT d.datname, 'primary', now() - interval '60 days', now() - interval '60 days',
       s.stats_reset, pg_postmaster_start_time(), now() - interval '1 day'
FROM pg_database d
JOIN pg_stat_database s ON s.datid = d.oid
WHERE d.datname IN ('aviorlineage', 'aviorfixture', 'aviordeploy')
ON CONFLICT DO NOTHING;

-- Daily growth: each database grew by 40% over the sixty days, with a step
-- three weeks ago so the sparkline has something to show.
INSERT INTO observatory_db_size_daily (database, day, size_bytes)
SELECT d.datname, (now() AT TIME ZONE 'UTC')::date - g,
       (pg_database_size(d.datname) * (1 - g / 150.0) * CASE WHEN g > 21 THEN 0.9 ELSE 1 END)::bigint
FROM pg_database d
CROSS JOIN generate_series(0, 59) AS g
WHERE d.datname IN ('aviorlineage', 'aviorfixture', 'aviordeploy')
ON CONFLICT DO NOTHING;

INSERT INTO observatory_run (started_at, finished_at, databases_read, databases_failed)
VALUES (now() - interval '1 day' - interval '2 seconds', now() - interval '1 day',
        ARRAY['aviordeploy', 'aviorfixture', 'aviorlineage'], ARRAY[]::text[]);

-- Thirty days of submissions for the usage tab and the overview: office
-- hours in Singapore on weekdays, a quiet weekend, a slow climb towards today,
-- seven operators over three databases, refusals under six rules, and a
-- latency spread with a few slow outliers. Every figure is a hash of the slot,
-- so a re-seed writes the same trail shifted to the new now.
INSERT INTO app_user (id, username, password_hash, totp_secret)
VALUES
    ('00000000-0000-0000-0000-0000000000d1', 'mira.chen', '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu', 'JBSWY3DPEHPK3PXP'),
    ('00000000-0000-0000-0000-0000000000d2', 'jonas.berg', '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu', 'JBSWY3DPEHPK3PXP'),
    ('00000000-0000-0000-0000-0000000000d3', 'ade.okafor', '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu', 'JBSWY3DPEHPK3PXP'),
    ('00000000-0000-0000-0000-0000000000d4', 'sofia.reyes', '$2a$10$rJ/x7rFHshpO8M2/SzBecOJz7Be0KeqwMwnXOnXle/g1IjgoDzvIu', 'JBSWY3DPEHPK3PXP')
ON CONFLICT DO NOTHING;

INSERT INTO app_user_role (user_id, role)
SELECT id::uuid, 'developer'
FROM unnest(ARRAY['00000000-0000-0000-0000-0000000000d1', '00000000-0000-0000-0000-0000000000d2',
                  '00000000-0000-0000-0000-0000000000d3', '00000000-0000-0000-0000-0000000000d4']) AS id
ON CONFLICT DO NOTHING;

DELETE FROM audit_entry
WHERE id IN (SELECT md5('observatory-demo-' || g)::uuid FROM generate_series(1, 12000) AS g);

WITH slots AS (
    SELECT h, s, date_trunc('hour', now()) - make_interval(hours => h) AS hour_start
    FROM generate_series(0, 30 * 24 - 1) AS h
    CROSS JOIN generate_series(0, 15) AS s
), rated AS (
    SELECT h, s, hour_start,
           CASE
               WHEN extract(hour FROM hour_start AT TIME ZONE 'UTC') BETWEEN 1 AND 9 THEN 15
               WHEN extract(hour FROM hour_start AT TIME ZONE 'UTC') BETWEEN 10 AND 13 THEN 6
               ELSE 0.6
           END
           * CASE WHEN extract(isodow FROM hour_start AT TIME ZONE 'UTC') >= 6 THEN 0.2 ELSE 1 END
           * (0.7 + 0.3 * (1 - h / 720.0)) AS rate
    FROM slots
), kept AS (
    SELECT h * 16 + s + 1 AS g, hour_start,
           abs(hashtext('op-' || h || '-' || s)) % 20 AS who,
           abs(hashtext('db-' || h || '-' || s)) % 10 AS db,
           abs(hashtext('verdict-' || h || '-' || s)) % 100 AS verdict,
           abs(hashtext('shape-' || h || '-' || s)) % 8 AS shape,
           abs(hashtext('lat-' || h || '-' || s)) % 1000 AS lat,
           abs(hashtext('rows-' || h || '-' || s)) % 1000 AS rws,
           abs(hashtext('at-' || h || '-' || s)) % 3600 AS second
    FROM rated
    WHERE s + (abs(hashtext('keep-' || h || '-' || s)) % 100) / 100.0 < rate
)
INSERT INTO audit_entry (id, user_id, instance_id, database, statement, decision, reasons, row_count, elapsed_ms, created_at)
SELECT md5('observatory-demo-' || k.g)::uuid,
       (CASE
            WHEN k.who < 5 THEN '00000000-0000-0000-0000-0000000000d1'
            WHEN k.who < 9 THEN '00000000-0000-0000-0000-0000000000d2'
            WHEN k.who < 12 THEN '00000000-0000-0000-0000-000000000001'
            WHEN k.who < 15 THEN '00000000-0000-0000-0000-0000000000d3'
            WHEN k.who < 17 THEN '00000000-0000-0000-0000-0000000000d4'
            WHEN k.who < 19 THEN '00000000-0000-0000-0000-000000000003'
            ELSE '00000000-0000-0000-0000-000000000005'
        END)::uuid,
       '00000000-0000-0000-0000-0000000000a1',
       CASE WHEN k.db < 5 THEN 'aviorlineage' WHEN k.db < 8 THEN 'aviorfixture' ELSE 'aviordeploy' END,
       (ARRAY['SELECT player_id, name, tier FROM public.player WHERE player_id = 42',
              'SELECT r.round_id, r.stake, r.payout FROM public.round r JOIN public.player p USING (player_id) WHERE p.tier = ''gold''',
              'SELECT wallet_id, sum(amount) FROM public.ledger GROUP BY wallet_id ORDER BY 2 DESC LIMIT 20',
              'SELECT day, stakes FROM reporting.daily_stakes WHERE day >= current_date - 7',
              'SELECT count(*) FROM public.round WHERE status = ''open''',
              'SELECT player_id, balance FROM public.wallet WHERE balance > 1000',
              'SELECT member_id, name FROM public.member LIMIT 100',
              'SELECT bet_id, amount FROM public.bet WHERE created_at > now() - interval ''1 day'''])[1 + k.shape],
       CASE WHEN k.verdict < 12 THEN 'deny' ELSE 'allow' END,
       CASE
           WHEN k.verdict >= 12 THEN '[]'::jsonb
           WHEN k.verdict < 3 THEN '[{"Rule": "column_denied", "Message": "caller is not permitted to read this column", "Subject": "public.member.id_card_number"}]'::jsonb
           WHEN k.verdict < 5 THEN '[{"Rule": "wildcard_select", "Message": "name the columns to read instead of *", "Subject": ""}]'::jsonb
           WHEN k.verdict < 7 THEN '[{"Rule": "statement_not_read_only", "Message": "statement must begin with SELECT or WITH, found \"UPDATE\"", "Subject": ""}]'::jsonb
           WHEN k.verdict < 9 THEN '[{"Rule": "cost_limit_exceeded", "Message": "estimated cost is over the limit", "Subject": ""}]'::jsonb
           WHEN k.verdict < 11 THEN '[{"Rule": "table_denied", "Message": "caller is not permitted to read this table", "Subject": "ops.adjustment"}]'::jsonb
           ELSE '[{"Rule": "row_limit_exceeded", "Message": "estimated rows are over the limit", "Subject": ""}]'::jsonb
       END,
       CASE
           WHEN k.verdict < 12 OR k.lat = 0 THEN NULL
           WHEN k.rws < 150 THEN 0
           WHEN k.rws < 900 THEN 1 + k.rws % 200
           ELSE 1000 + k.rws * 37
       END,
       CASE
           WHEN k.verdict < 12 OR k.lat = 0 THEN NULL
           WHEN k.lat < 600 THEN 2 + k.lat % 40
           WHEN k.lat < 900 THEN 40 + (k.lat * 7) % 260
           WHEN k.lat < 990 THEN 300 + (k.lat * 13) % 1700
           ELSE 5000 + (k.lat * 97) % 25000
       END,
       k.hour_start + make_interval(secs => k.second)
FROM kept k
WHERE k.hour_start + make_interval(secs => k.second) < now();
