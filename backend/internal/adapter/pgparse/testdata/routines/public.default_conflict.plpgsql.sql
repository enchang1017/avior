CREATE OR REPLACE FUNCTION public.default_conflict(stake numeric)
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
DECLARE n bigint;
BEGIN
  SELECT count(*) INTO n FROM bet WHERE payout > stake;
  RETURN n;
END $function$

