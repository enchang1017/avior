CREATE OR REPLACE FUNCTION public.expression_calls()
 RETURNS numeric
 LANGUAGE plpgsql
AS $function$
BEGIN
  PERFORM log_event('x', 1);
  RETURN member_total_stake(promo.helper());
END $function$

