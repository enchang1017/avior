CREATE OR REPLACE FUNCTION public.cte_write()
 RETURNS bigint
 LANGUAGE plpgsql
AS $function$
DECLARE n bigint;
BEGIN
  WITH moved AS (DELETE FROM bet_audit WHERE at < now() RETURNING bet_id)
  SELECT count(*) INTO n FROM moved;
  RETURN n;
END $function$

