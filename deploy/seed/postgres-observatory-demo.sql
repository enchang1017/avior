-- FEAT-071's demo routines, added to the lineage demo database: one family of
-- date-versioned copies as the estate grows them, one family with an overload,
-- a name whose suffix is not a date, and a LANGUAGE sql routine that
-- track_functions = pl does not count. Local development only.

CREATE SCHEMA payout;

CREATE FUNCTION payout.shop_settle_order_26_09_19(p_round integer) RETURNS integer
LANGUAGE plpgsql AS $$ BEGIN RETURN p_round + 19; END $$;
CREATE FUNCTION payout.shop_settle_order_26_09_21(p_round integer) RETURNS integer
LANGUAGE plpgsql AS $$ BEGIN RETURN p_round + 21; END $$;
CREATE FUNCTION payout.shop_settle_order_26_09_23(p_round integer) RETURNS integer
LANGUAGE plpgsql AS $$ BEGIN RETURN p_round + 23; END $$;
CREATE FUNCTION payout.shop_settle_order_26_09_24(p_round integer) RETURNS integer
LANGUAGE plpgsql AS $$ BEGIN RETURN p_round + 24; END $$;
CREATE FUNCTION payout.shop_settle_order_26_09_25(p_round integer) RETURNS integer
LANGUAGE plpgsql AS $$ BEGIN RETURN p_round + 25; END $$;

-- Not a version: there is no thirteenth month.
CREATE FUNCTION payout.shop_settle_order_26_13_40(p_round integer) RETURNS integer
LANGUAGE plpgsql AS $$ BEGIN RETURN p_round; END $$;

CREATE FUNCTION payout.settle_bonus_26_08_01(p_player integer) RETURNS numeric
LANGUAGE plpgsql AS $$ BEGIN RETURN p_player * 1.0; END $$;
CREATE FUNCTION payout.settle_bonus_26_09_10(p_player integer) RETURNS numeric
LANGUAGE plpgsql AS $$ BEGIN RETURN p_player * 1.5; END $$;
CREATE FUNCTION payout.settle_bonus_26_09_10(p_player bigint) RETURNS numeric
LANGUAGE plpgsql AS $$ BEGIN RETURN p_player * 1.5; END $$;

CREATE PROCEDURE payout.close_round_26_09_01(p_round integer)
LANGUAGE plpgsql AS $$ BEGIN PERFORM p_round; END $$;

CREATE FUNCTION payout.round_total(p_round integer) RETURNS integer
LANGUAGE sql AS 'SELECT p_round * 2';

GRANT USAGE ON SCHEMA payout TO avior_governed;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA payout TO avior_governed;
GRANT EXECUTE ON ALL PROCEDURES IN SCHEMA payout TO avior_governed;

-- FEAT-072. Enough rows that the largest tables and the size lines have
-- something to compare. The trigger on round writes round_audit as it goes.
INSERT INTO public.round (player_id, stake, payout, status)
SELECT 1 + g % 2, 1 + g % 500,
       CASE WHEN g % 3 = 0 THEN NULL ELSE g % 700 END,
       CASE WHEN g % 3 = 0 THEN 'open' ELSE 'settled' END
FROM generate_series(1, 150000) AS g;

INSERT INTO public.ledger (wallet_id, amount, note)
SELECT w.wallet_id, g % 900 - 450, 'demo'
FROM generate_series(1, 250000) AS g
JOIN public.wallet w ON w.player_id = 1 + g % 2;

ANALYZE;
