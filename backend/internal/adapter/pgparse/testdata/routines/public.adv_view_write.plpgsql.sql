CREATE OR REPLACE FUNCTION public.adv_view_write(p bigint)
 RETURNS void
 LANGUAGE plpgsql
AS $function$
BEGIN
  UPDATE bet_simple SET stake = 0 WHERE bet_id = p;
END $function$

