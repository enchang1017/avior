CREATE OR REPLACE FUNCTION public.set_forms(p bigint)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  UPDATE bet SET stake = payout, (status, settled_at) = ('x', now()) WHERE id = p;
  DELETE FROM account a USING member m WHERE a.member_id = m.id AND m.closed;
END $function$

