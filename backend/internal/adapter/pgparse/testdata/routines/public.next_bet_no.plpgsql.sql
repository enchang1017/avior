CREATE OR REPLACE FUNCTION public.next_bet_no()
 RETURNS bigint
 LANGUAGE plpgsql
 SET search_path TO 'public'
AS $function$
BEGIN
  RETURN nextval('bet_no_seq');
END $function$

