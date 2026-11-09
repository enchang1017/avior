CREATE OR REPLACE FUNCTION public.settle_bet(p_bet bigint, p_payout numeric)
 RETURNS numeric
 LANGUAGE plpgsql
AS $function$
DECLARE
  v_member bigint;
  v_stake  numeric;
  v_new    numeric;
BEGIN
  SELECT b.member_id, b.stake INTO v_member, v_stake FROM bet b WHERE b.id = p_bet AND b.status = 'open';
  IF NOT FOUND THEN
    RAISE EXCEPTION 'bet % not open', p_bet;
  END IF;
  UPDATE bet SET status = 'settled', payout = p_payout, settled_at = now() WHERE id = p_bet;
  WITH credited AS (
    UPDATE member SET balance = balance + p_payout WHERE id = v_member RETURNING id, balance
  )
  INSERT INTO ledger (member_id, amount, kind, booked_on)
  SELECT c.id, p_payout, 'payout', current_date FROM credited c;
  SELECT balance INTO v_new FROM member WHERE id = v_member;
  PERFORM log_event('settle', p_bet);
  RETURN v_new;
END $function$

