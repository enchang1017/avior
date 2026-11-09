CREATE OR REPLACE FUNCTION public.pg18_returning(p bigint)
 RETURNS numeric
 LANGUAGE plpgsql
AS $function$
DECLARE v numeric;
BEGIN
  UPDATE bet SET stake = 0 WHERE id = p RETURNING WITH (OLD AS o) o.stake INTO v;
  RETURN v;
END $function$

