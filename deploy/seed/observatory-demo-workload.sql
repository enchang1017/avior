-- Run as avior_governed against the lineage demo database, so the Performance
-- tab has operator statements to rank: cheap lookups called often, joins and
-- aggregates in between, and a few slow ones, one sleeping on purpose. Every
-- statement is top level, which is all pg_stat_statements counts by default.
-- Local development only; the seed runs it on every start, so the counters
-- only grow.

SELECT format('SELECT player_id, name, tier FROM public.player WHERE player_id = %s', g % 4 + 1)
FROM generate_series(1, 240) AS g
\gexec

SELECT format('SELECT w.wallet_id, w.balance FROM public.wallet w WHERE w.player_id = %s', g % 4 + 1)
FROM generate_series(1, 120) AS g
\gexec

SELECT format(
    'SELECT p.tier, count(r.round_id), coalesce(sum(r.stake), 0) FROM public.player p '
    'LEFT JOIN public.round r ON r.player_id = p.player_id WHERE p.player_id > %s GROUP BY p.tier', g % 3)
FROM generate_series(1, 60) AS g
\gexec

SELECT format(
    'SELECT l.wallet_id, sum(l.amount) FROM public.ledger l JOIN public.wallet w USING (wallet_id) '
    'WHERE w.balance >= %s GROUP BY l.wallet_id ORDER BY 2 DESC LIMIT 20', g * 10)
FROM generate_series(1, 40) AS g
\gexec

SELECT format(
    'SELECT player_id, stake, rank() OVER (PARTITION BY player_id ORDER BY stake DESC) '
    'FROM public.round WHERE stake > %s', g)
FROM generate_series(1, 25) AS g
\gexec

SELECT format('SELECT count(*), sum(x) FROM generate_series(1, %s) AS x', 1500000 + g)
FROM generate_series(1, 6) AS g
\gexec

SELECT format('SELECT md5(string_agg(x::text, '','')) FROM generate_series(1, %s) AS x', 400000 + g)
FROM generate_series(1, 4) AS g
\gexec

SELECT format('SELECT pg_sleep(%s), count(*) FROM public.player', 0.2 + g * 0.3)
FROM generate_series(1, 3) AS g
\gexec

SELECT format(
    'SELECT pg_sleep(%s), p.name, w.balance FROM public.player p JOIN public.wallet w USING (player_id) '
    'WHERE p.player_id = %s', 1.5, g)
FROM generate_series(1, 2) AS g
\gexec

-- FEAT-073. Top-level calls that are exactly one call of a routine, so the
-- Routine timing tab has a slowest call to show; the pair in one statement is
-- never matched to either routine.
SELECT format('SELECT payout.shop_settle_order_26_09_24(%s)', g)
FROM generate_series(1, 30) AS g
\gexec

SELECT format('SELECT * FROM payout.settle_bonus_26_09_10(%s)', g)
FROM generate_series(1, 10) AS g
\gexec

SELECT format('SELECT player_email(%s)', g % 2 + 1)
FROM generate_series(1, 12) AS g
\gexec

SELECT 'SELECT payout.shop_settle_order_26_13_40(1), payout.shop_settle_order_26_13_40(2)'
\gexec
