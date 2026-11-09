CREATE OR REPLACE FUNCTION public.use_column_fn(stake numeric)
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
#variable_conflict use_column
DECLARE n bigint;
BEGIN
  SELECT count(*) INTO n FROM bet WHERE payout > stake;
  RETURN n;
END $function$

