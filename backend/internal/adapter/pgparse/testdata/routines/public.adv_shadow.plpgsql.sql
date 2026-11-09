CREATE OR REPLACE FUNCTION public.adv_shadow(stake numeric)
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
#variable_conflict use_variable
DECLARE n bigint;
BEGIN
  SELECT count(*) INTO n FROM bet WHERE bet.status = 'open' AND bet.payout > stake;
  RETURN n;
END $function$

