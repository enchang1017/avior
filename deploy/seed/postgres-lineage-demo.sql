-- A governed database with enough lineage to show FEAT-070 on the local stack:
-- foreign keys with a cascade, a view over a view, a materialised view, a
-- trigger, a policy, a generated column, PL/pgSQL functions and procedures
-- that read and write, a BEGIN ATOMIC function, an overload, and a routine
-- that builds its SQL at run time. Local development only: the acceptance
-- suite builds its own shapes and never sees this database.

CREATE TABLE public.player (
    player_id  integer PRIMARY KEY,
    name       text    NOT NULL,
    email      text,
    tier       text    NOT NULL DEFAULT 'bronze'
);

CREATE TABLE public.wallet (
    wallet_id  bigserial PRIMARY KEY,
    player_id  integer   NOT NULL REFERENCES public.player (player_id) ON DELETE CASCADE,
    balance    numeric   NOT NULL DEFAULT 0
);

CREATE TABLE public.round (
    round_id   bigserial PRIMARY KEY,
    player_id  integer   NOT NULL REFERENCES public.player (player_id),
    stake      numeric   NOT NULL,
    payout     numeric,
    status     text      NOT NULL DEFAULT 'open',
    net        numeric   GENERATED ALWAYS AS (coalesce(payout, 0) - stake) STORED
);

CREATE TABLE public.ledger (
    entry_id   bigserial PRIMARY KEY,
    wallet_id  bigint    NOT NULL REFERENCES public.wallet (wallet_id),
    round_id   bigint    REFERENCES public.round (round_id),
    amount     numeric   NOT NULL,
    note       text
);

CREATE TABLE public.round_audit (
    audit_id   bigserial PRIMARY KEY,
    round_id   bigint,
    status     text,
    noted_at   timestamptz NOT NULL DEFAULT now()
);

ALTER TABLE public.round ENABLE ROW LEVEL SECURITY;
CREATE POLICY round_visible ON public.round USING (status <> 'void');

CREATE VIEW public.open_rounds AS
    SELECT r.round_id, r.player_id, r.stake, p.name
    FROM public.round r JOIN public.player p ON p.player_id = r.player_id
    WHERE r.status = 'open';

CREATE VIEW public.big_open_rounds AS
    SELECT round_id, name, stake FROM public.open_rounds WHERE stake > 100;

CREATE MATERIALIZED VIEW public.player_totals AS
    SELECT player_id, sum(stake) AS staked, sum(net) AS net
    FROM public.round GROUP BY player_id;

CREATE FUNCTION public.log_round() RETURNS trigger LANGUAGE plpgsql SET search_path = public AS $$
BEGIN
  INSERT INTO round_audit (round_id, status) VALUES (NEW.round_id, NEW.status);
  RETURN NEW;
END $$;

CREATE TRIGGER round_logged AFTER INSERT OR UPDATE OF status ON public.round
    FOR EACH ROW EXECUTE FUNCTION public.log_round();

CREATE FUNCTION public.place_round(p_player integer, p_stake numeric) RETURNS bigint
LANGUAGE plpgsql SET search_path = public AS $$
DECLARE
  new_round bigint;
  w bigint;
BEGIN
  SELECT wallet_id INTO w FROM wallet WHERE player_id = p_player;
  UPDATE wallet SET balance = balance - p_stake WHERE wallet_id = w;
  INSERT INTO round (player_id, stake) VALUES (p_player, p_stake) RETURNING round_id INTO new_round;
  INSERT INTO ledger (wallet_id, round_id, amount, note) VALUES (w, new_round, -p_stake, 'stake');
  RETURN new_round;
END $$;

CREATE PROCEDURE public.settle_round(p_round bigint, p_payout numeric)
LANGUAGE plpgsql SET search_path = public AS $$
DECLARE
  w bigint;
BEGIN
  UPDATE round SET payout = p_payout, status = 'settled' WHERE round_id = p_round;
  SELECT wl.wallet_id INTO w FROM wallet wl JOIN round r ON r.player_id = wl.player_id WHERE r.round_id = p_round;
  UPDATE wallet SET balance = balance + p_payout WHERE wallet_id = w;
  INSERT INTO ledger (wallet_id, round_id, amount, note) VALUES (w, p_round, p_payout, 'payout');
END $$;

CREATE FUNCTION public.void_round(p_round bigint) RETURNS void LANGUAGE sql
BEGIN ATOMIC
  UPDATE public.round SET status = 'void' WHERE round_id = p_round;
END;

CREATE FUNCTION public.player_email(p_player integer) RETURNS text
LANGUAGE plpgsql SET search_path = public AS $$
DECLARE
  e text;
BEGIN
  SELECT email INTO e FROM player WHERE player_id = p_player;
  RETURN e;
END $$;

CREATE FUNCTION public.fee(p_stake numeric) RETURNS numeric LANGUAGE sql AS 'SELECT p_stake * 0.02';
CREATE FUNCTION public.fee(p_stake numeric, p_tier text) RETURNS numeric LANGUAGE sql AS 'SELECT p_stake * 0.01';

CREATE FUNCTION public.archive_table(p_table text) RETURNS void
LANGUAGE plpgsql SET search_path = public AS $$
BEGIN
  EXECUTE format('DELETE FROM %I WHERE noted_at < now() - interval ''90 days''', p_table);
END $$;

CREATE PROCEDURE public.nightly_close()
LANGUAGE plpgsql SET search_path = public AS $$
BEGIN
  UPDATE round SET status = 'void' WHERE status = 'open' AND stake = 0;
  PERFORM archive_table('round_audit');
  REFRESH MATERIALIZED VIEW player_totals;
END $$;

-- Two more schemas, so the overview has a galaxy to draw: reporting reads
-- public, and ops writes into it.
CREATE SCHEMA reporting;
CREATE VIEW reporting.daily_stakes AS
    SELECT date_trunc('day', a.noted_at) AS day, count(*) AS rounds
    FROM public.round_audit a GROUP BY 1;
CREATE VIEW reporting.player_standing AS
    SELECT p.player_id, p.name, t.staked, t.net
    FROM public.player p JOIN public.player_totals t ON t.player_id = p.player_id;
CREATE FUNCTION reporting.top_players(p_limit integer) RETURNS SETOF reporting.player_standing
LANGUAGE sql STABLE SET search_path = reporting, public AS $$
  SELECT * FROM player_standing ORDER BY net DESC LIMIT p_limit
$$;

CREATE SCHEMA ops;
CREATE TABLE ops.adjustment (
    adjustment_id bigserial PRIMARY KEY,
    wallet_id     bigint  NOT NULL REFERENCES public.wallet (wallet_id),
    amount        numeric NOT NULL,
    reason        text
);
CREATE PROCEDURE ops.apply_adjustment(p_wallet bigint, p_amount numeric, p_reason text)
LANGUAGE plpgsql SET search_path = ops, public AS $$
BEGIN
  INSERT INTO adjustment (wallet_id, amount, reason) VALUES (p_wallet, p_amount, p_reason);
  UPDATE wallet SET balance = balance + p_amount WHERE wallet_id = p_wallet;
END $$;

INSERT INTO public.player VALUES (1, 'ada', 'ada@example.test', 'gold'), (2, 'bo', 'bo@example.test', 'bronze');
INSERT INTO public.wallet (player_id, balance) VALUES (1, 500), (2, 50);
REFRESH MATERIALIZED VIEW public.player_totals;
ANALYZE;

GRANT CONNECT ON DATABASE aviorlineage TO avior_governed;
GRANT USAGE ON SCHEMA public, reporting, ops TO avior_governed;
GRANT SELECT ON ALL TABLES IN SCHEMA public, reporting, ops TO avior_governed;
GRANT EXECUTE ON ALL FUNCTIONS IN SCHEMA public, reporting, ops TO avior_governed;
GRANT EXECUTE ON ALL PROCEDURES IN SCHEMA public, reporting, ops TO avior_governed;
