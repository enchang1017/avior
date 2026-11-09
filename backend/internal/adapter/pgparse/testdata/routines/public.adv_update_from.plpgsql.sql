CREATE OR REPLACE FUNCTION public.adv_update_from()
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
DECLARE v bigint;
BEGIN
  UPDATE bet b SET payout = m.balance FROM member m
  WHERE b.member_id = m.id AND m.closed RETURNING b.id INTO v;
  RETURN v;
END $function$

